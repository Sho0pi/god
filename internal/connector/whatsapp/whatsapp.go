package whatsapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
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

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
)

const (
	sqliteDriver        = "sqlite"
	dbName              = "store.db"
	reconnectInitial    = 5 * time.Second
	reconnectMax        = 5 * time.Minute
	reconnectMultiplier = 2.0
)

type Connector struct {
	storePath string
	configFn  func() *config.Config // always returns latest config
	// allowSource, when set, returns extra allow-list entries from the store
	// (managed at runtime via the /allow admin command). Merged with yaml.
	allowSource  func() []string
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

// New creates a WhatsApp connector. configFn is called on each message to get
// the latest config — changes to god.yaml take effect immediately.
func New(storePath string, configFn func() *config.Config) *Connector {
	norm := normalizeAllow(configFn().Connectors.WhatsApp.Allow)
	if len(norm) == 0 {
		log.Println("whatsapp: allow list empty — accepting all senders")
	} else {
		log.Printf("whatsapp: allow list: %v", norm)
	}
	return &Connector{storePath: storePath, configFn: configFn}
}

func normalizeAllow(allow []string) []string {
	norm := make([]string, 0, len(allow))
	for _, n := range allow {
		if d := digitsOnly(n); d != "" {
			norm = append(norm, d)
		}
	}
	return norm
}

// SetAllowSource registers a function returning runtime allow-list entries
// (e.g. store-backed numbers added via /allow). Entries are merged with the
// yaml allow list. Call before Start.
func (c *Connector) SetAllowSource(fn func() []string) {
	c.allowSource = fn
}

func (c *Connector) isAllowed(senderUser string) bool {
	allow := normalizeAllow(c.configFn().Connectors.WhatsApp.Allow)
	if c.allowSource != nil {
		allow = append(allow, normalizeAllow(c.allowSource())...)
	}
	if len(allow) == 0 {
		return true
	}
	normalized := digitsOnly(senderUser)
	for _, a := range allow {
		if phoneMatch(a, normalized) {
			return true
		}
	}
	return false
}

// phoneMatch compares two digit-only phone numbers tolerantly. WhatsApp reports
// senders in full international form (e.g. 972501234567) while allow-list entries
// are often written in local form (0501234567), where the leading 0 is a trunk
// prefix that the country code replaces. Strip leading zeros from both, then treat
// them as equal when one is a suffix of the other, requiring >=7 shared trailing
// digits to avoid false matches between unrelated numbers.
func phoneMatch(a, b string) bool {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) < 7 {
		return false
	}
	return strings.HasSuffix(long, short)
}

// senderPhone returns the phone number of the sender.
// When SenderAlt is set, the Sender JID is a LID (device ID) — SenderAlt holds the real phone.
func senderPhone(src types.MessageSource) string {
	if !src.SenderAlt.IsEmpty() {
		return src.SenderAlt.User
	}
	return src.Sender.User
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
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
	db, err := sql.Open(sqliteDriver, "file:"+dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return fmt.Errorf("enable foreign keys: %w", err)
	}

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

	// Clear the typing indicator before the reply lands.
	if err := client.SendChatPresence(ctx, to, types.ChatPresencePaused, types.ChatPresenceMediaText); err != nil {
		log.Printf("whatsapp: clear typing: %v", err)
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
	case *events.Connected:
		// Announce presence so the server has our pushname and typing
		// indicators we send later are actually delivered to peers.
		c.mu.Lock()
		client := c.client
		c.mu.Unlock()
		if client != nil {
			if err := client.SendPresence(c.runCtx, types.PresenceAvailable); err != nil {
				log.Printf("whatsapp: send presence: %v", err)
			}
		}
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

	src := evt.Info.MessageSource
	if !c.isAllowed(senderPhone(src)) {
		log.Printf("whatsapp: blocked %q (not in allow list)", senderPhone(src))
		return
	}

	text := extractText(evt.Message)
	if text == "" {
		return
	}

	// Group message handling.
	if src.IsGroup {
		c.mu.Lock()
		client := c.client
		c.mu.Unlock()

		isMentioned, trimmed := c.isMentionedInGroup(evt.Message, client)
		ok, text2 := c.shouldRespondInGroup(isMentioned, trimmed)
		if !ok {
			return
		}
		text = text2
	}

	if c.handler == nil {
		return
	}

	phone := senderPhone(evt.Info.MessageSource)
	msg := connector.Message{
		Connector: "whatsapp",
		UserID:    phone,
		ChatID:    evt.Info.Chat.String(),
		SenderID:  evt.Info.Sender.String(),
		Text:      text,
	}

	log.Printf("whatsapp: msg from %s: %q", msg.SenderID, truncate(text, 60))
	c.acknowledge(evt) // blue ticks + typing indicator while god thinks
	go c.handler(c.runCtx, msg)
}

// acknowledge marks the incoming message as read and shows a typing indicator
// so the sender sees that god received the message and is working on a reply.
// Best-effort: failures are logged but never block message handling.
func (c *Connector) acknowledge(evt *events.Message) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return
	}
	info := evt.Info
	if err := client.MarkRead(c.runCtx, []types.MessageID{info.ID}, time.Now(), info.Chat, info.Sender); err != nil {
		log.Printf("whatsapp: mark read: %v", err)
	}
	if err := client.SendChatPresence(c.runCtx, info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText); err != nil {
		log.Printf("whatsapp: typing presence: %v", err)
	}
}

func extractText(msg *waE2E.Message) string {
	if t := strings.TrimSpace(msg.GetConversation()); t != "" {
		return t
	}
	if msg.ExtendedTextMessage != nil {
		if t := strings.TrimSpace(msg.ExtendedTextMessage.GetText()); t != "" {
			return t
		}
	}
	if loc := msg.GetLiveLocationMessage(); loc != nil {
		lat, lng := loc.GetDegreesLatitude(), loc.GetDegreesLongitude()
		if isFinite(lat) && isFinite(lng) {
			body := fmt.Sprintf("🛰 Live location: %s%s", formatCoords(lat, lng), formatAccuracy(loc.GetAccuracyInMeters()))
			if meta := locationMetaBlock(lat, lng, "", "", loc.GetCaption()); meta != "" {
				body += "\n" + meta
			}
			return body
		}
	}
	if loc := msg.GetLocationMessage(); loc != nil {
		lat, lng := loc.GetDegreesLatitude(), loc.GetDegreesLongitude()
		if isFinite(lat) && isFinite(lng) {
			var body string
			if loc.GetIsLive() {
				body = fmt.Sprintf("🛰 Live location: %s%s", formatCoords(lat, lng), formatAccuracy(loc.GetAccuracyInMeters()))
			} else {
				body = fmt.Sprintf("📍 %s%s", formatCoords(lat, lng), formatAccuracy(loc.GetAccuracyInMeters()))
			}
			if meta := locationMetaBlock(lat, lng, loc.GetName(), loc.GetAddress(), loc.GetComment()); meta != "" {
				body += "\n" + meta
			}
			return body
		}
	}
	return ""
}

func isFinite(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}

func formatCoords(lat, lng float64) string {
	return fmt.Sprintf("%.6f, %.6f", lat, lng)
}

func formatAccuracy(meters uint32) string {
	if meters == 0 {
		return ""
	}
	return fmt.Sprintf(" ±%dm", meters)
}

func locationMetaBlock(lat, lng float64, name, address, caption string) string {
	if name == "" && address == "" && caption == "" {
		return ""
	}
	type meta struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name,omitempty"`
		Address   string  `json:"address,omitempty"`
		Caption   string  `json:"caption,omitempty"`
	}
	b, _ := json.Marshal(meta{Latitude: lat, Longitude: lng, Name: name, Address: address, Caption: caption})
	return "Location (untrusted metadata):\n```json\n" + string(b) + "\n```"
}

// isMentionedInGroup returns true if the bot's own JID appears in the message's mention list.
// It also returns the text with the @mention stripped.
func (c *Connector) isMentionedInGroup(msg *waE2E.Message, client *whatsmeow.Client) (bool, string) {
	text := extractText(msg)
	if client == nil || client.Store.ID == nil {
		return false, text
	}

	var mentionedJIDs []string
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.ContextInfo != nil {
		mentionedJIDs = msg.ExtendedTextMessage.ContextInfo.MentionedJID
	}

	// Collect both the phone-number user and the LID user so we match
	// regardless of which addressing mode WhatsApp used for the @mention.
	botPN := client.Store.ID.User
	botLID := client.Store.GetLID().User

	for _, raw := range mentionedJIDs {
		j, err := types.ParseJID(raw)
		if err != nil {
			continue
		}
		if j.User == botPN || (botLID != "" && j.User == botLID) {
			// Strip @<user> from the text — WhatsApp encodes as @<LID or PN>.
			cleaned := strings.ReplaceAll(text, "@"+j.User, "")
			return true, strings.TrimSpace(cleaned)
		}
	}
	return false, text
}

// shouldRespondInGroup applies group_trigger config — mirrors picoclaw's ShouldRespondInGroup.
func (c *Connector) shouldRespondInGroup(isMentioned bool, content string) (bool, string) {
	gt := c.configFn().Connectors.WhatsApp.GroupTrigger

	if isMentioned {
		return true, content
	}
	if gt.MentionOnly {
		return false, content
	}
	if len(gt.Prefixes) > 0 {
		for _, prefix := range gt.Prefixes {
			if prefix != "" && strings.HasPrefix(content, prefix) {
				return true, strings.TrimSpace(strings.TrimPrefix(content, prefix))
			}
		}
		return false, content
	}
	return true, content
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
