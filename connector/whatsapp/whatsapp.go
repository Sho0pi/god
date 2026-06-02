package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"

	"github.com/sho0pi/god/connector"
)

const (
	sqliteDriver        = "sqlite"
	dbName              = "store.db"
	reconnectInitial    = 5 * time.Second
	reconnectMax        = 5 * time.Minute
	reconnectMultiplier = 2.0
)

type Connector struct {
	storePath    string
	handler      func(ctx context.Context, msg connector.Message)
	client       *whatsmeow.Client
	container    *sqlstore.Container
	mu           sync.Mutex
	runCtx       context.Context
	runCancel    context.CancelFunc
	reconnMu     sync.Mutex
	reconnecting bool
	stopping     atomic.Bool
	wg           sync.WaitGroup
}

func New(storePath string) *Connector {
	return &Connector{storePath: storePath}
}

func (c *Connector) SetMessageHandler(handler func(ctx context.Context, msg connector.Message)) {
	c.handler = handler
}

func (c *Connector) Start(ctx context.Context) error {
	log.Printf("whatsapp: starting, store=%s", c.storePath)

	c.stopping.Store(false)
	c.runCtx, c.runCancel = context.WithCancel(ctx)

	if err := os.MkdirAll(c.storePath, 0o700); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}

	dbPath := filepath.Join(c.storePath, dbName)
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	waLogger := waLog.Stdout("WhatsApp", "WARN", true)
	container := sqlstore.NewWithDB(db, sqliteDriver, waLogger)
	if err = container.Upgrade(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("upgrade db: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return fmt.Errorf("get device: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLogger)
	client.AddEventHandler(c.eventHandler)

	c.mu.Lock()
	c.container = container
	c.client = client
	c.mu.Unlock()

	if client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(c.runCtx)
		if err != nil {
			c.mu.Lock()
			c.client = nil
			c.container = nil
			c.mu.Unlock()
			_ = container.Close()
			return fmt.Errorf("get qr channel: %w", err)
		}
		if err := client.Connect(); err != nil {
			c.mu.Lock()
			c.client = nil
			c.container = nil
			c.mu.Unlock()
			_ = container.Close()
			return fmt.Errorf("connect: %w", err)
		}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.runCtx.Done():
					return
				case evt, open := <-qrChan:
					if !open {
						return
					}
					if evt.Event == "code" {
						log.Println("whatsapp: scan QR code with WhatsApp → Linked Devices")
						qrterminal.GenerateWithConfig(evt.Code, qrterminal.Config{
							Level:      qrterminal.L,
							Writer:     os.Stdout,
							HalfBlocks: true,
						})
					} else {
						log.Printf("whatsapp: login event: %s", evt.Event)
					}
				}
			}
		}()
	} else {
		if err := client.Connect(); err != nil {
			c.mu.Lock()
			c.client = nil
			c.container = nil
			c.mu.Unlock()
			_ = container.Close()
			return fmt.Errorf("connect: %w", err)
		}
	}

	log.Println("whatsapp: connected, waiting for messages")
	return nil
}

func (c *Connector) Stop(ctx context.Context) error {
	log.Println("whatsapp: stopping")
	c.stopping.Store(true)

	if c.runCancel != nil {
		c.runCancel()
	}

	c.mu.Lock()
	client := c.client
	container := c.container
	c.mu.Unlock()

	if client != nil {
		client.Disconnect()
	}

	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}

	c.mu.Lock()
	c.client = nil
	c.container = nil
	c.mu.Unlock()

	if container != nil {
		_ = container.Close()
	}
	return nil
}

func (c *Connector) Send(ctx context.Context, chatID, text string) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	to, err := parseJID(chatID)
	if err != nil {
		return fmt.Errorf("invalid jid %q: %w", chatID, err)
	}

	_, err = client.SendMessage(ctx, to, &waE2E.Message{
		Conversation: proto.String(text),
	})
	return err
}

func (c *Connector) eventHandler(evt any) {
	switch v := evt.(type) {
	case *events.Message:
		c.handleIncoming(v)
	case *events.Disconnected:
		c.reconnMu.Lock()
		if c.reconnecting || c.stopping.Load() {
			c.reconnMu.Unlock()
			return
		}
		c.reconnecting = true
		c.wg.Add(1)
		c.reconnMu.Unlock()
		go func() {
			defer c.wg.Done()
			c.reconnectWithBackoff()
		}()
	}
}

func (c *Connector) reconnectWithBackoff() {
	defer func() {
		c.reconnMu.Lock()
		c.reconnecting = false
		c.reconnMu.Unlock()
	}()

	backoff := reconnectInitial
	for {
		select {
		case <-c.runCtx.Done():
			return
		default:
		}
		c.mu.Lock()
		client := c.client
		c.mu.Unlock()
		if client == nil {
			return
		}
		log.Printf("whatsapp: reconnecting (backoff=%s)", backoff)
		if err := client.Connect(); err == nil {
			log.Println("whatsapp: reconnected")
			return
		}
		select {
		case <-c.runCtx.Done():
			return
		case <-time.After(backoff):
			next := time.Duration(float64(backoff) * reconnectMultiplier)
			if next > reconnectMax {
				next = reconnectMax
			}
			backoff = next
		}
	}
}

func (c *Connector) handleIncoming(evt *events.Message) {
	if evt.Info.IsFromMe || evt.Message == nil {
		return
	}

	text := evt.Message.GetConversation()
	if text == "" && evt.Message.ExtendedTextMessage != nil {
		text = evt.Message.ExtendedTextMessage.GetText()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	if c.handler == nil {
		return
	}

	msg := connector.Message{
		ChatID:   evt.Info.Chat.String(),
		SenderID: evt.Info.Sender.String(),
		Text:     text,
	}

	log.Printf("whatsapp: msg from %s: %q", msg.SenderID, truncate(text, 60))
	go c.handler(c.runCtx, msg)
}

func parseJID(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.JID{}, fmt.Errorf("empty jid")
	}
	if strings.Contains(s, "@") {
		return types.ParseJID(s)
	}
	return types.NewJID(s, types.DefaultUserServer), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
