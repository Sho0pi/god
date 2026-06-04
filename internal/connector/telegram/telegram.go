// Package telegram implements a connector.Connector backed by the Telegram Bot
// API (via github.com/mymmrac/telego). It receives messages through long
// polling and sends replies, splitting them to respect Telegram's 4096-char
// per-message limit. Like the WhatsApp connector it supports an allow-list
// (numeric Telegram user IDs) and a group-chat trigger (respond only when
// mentioned). Media, streaming/draft messages, and rich markdown are out of
// scope — see the tracking GitHub issues.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/mymmrac/telego"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
)

// connectorName is the identity reported on every message. Soul/role/memory are
// namespaced by this string. (Multiple bots with distinct personalities will
// need distinct names — tracked separately.)
const connectorName = "telegram"

// maxMessageLen is Telegram's hard limit on a single text message, in UTF-16
// code units. We split on rune boundaries below it, which is always safe since
// a rune is at most 2 UTF-16 units and we leave headroom.
const maxMessageLen = 4096

// sender is the subset of *telego.Bot the connector needs to send replies. It
// exists so Send and its chunking can be unit-tested with a fake.
type sender interface {
	SendMessage(ctx context.Context, params *telego.SendMessageParams) (*telego.Message, error)
}

type Connector struct {
	token    string
	configFn func() *config.Config // always returns latest config

	// allowSource, when set, returns extra allow-list entries from the store
	// (managed at runtime via the /allow admin command). Merged with yaml.
	allowSource func() []string

	handler func(ctx context.Context, msg connector.Message)

	bot      *telego.Bot
	send     sender
	username string // bot's @username, for mention detection in groups

	runCancel context.CancelFunc
	wg        sync.WaitGroup
}

// New creates a Telegram connector. token is the bot token from @BotFather.
// configFn is called on each message to get the latest config, so god.yaml
// edits take effect immediately.
func New(token string, configFn func() *config.Config) *Connector {
	if allow := configFn().Connectors.Telegram.Allow; len(allow) == 0 {
		slog.Info("telegram: allow list empty — accepting all senders")
	} else {
		slog.Info("telegram: allow list", "ids", allow)
	}
	return &Connector{token: token, configFn: configFn}
}

// SetAllowSource registers a function returning runtime allow-list entries
// (store-backed user IDs added via /allow), merged with the yaml allow list.
// Call before Start.
func (c *Connector) SetAllowSource(fn func() []string) {
	c.allowSource = fn
}

func (c *Connector) SetMessageHandler(handler func(ctx context.Context, msg connector.Message)) {
	c.handler = handler
}

// isAllowed reports whether a numeric Telegram user ID may use the bot. An empty
// merged allow-list accepts everyone; otherwise the ID must match exactly.
func (c *Connector) isAllowed(userID string) bool {
	allow := c.configFn().Connectors.Telegram.Allow
	if c.allowSource != nil {
		allow = append(append([]string{}, allow...), c.allowSource()...)
	}
	if len(allow) == 0 {
		return true
	}
	for _, a := range allow {
		if strings.TrimSpace(a) == userID {
			return true
		}
	}
	return false
}

func (c *Connector) Start(ctx context.Context) error {
	slog.Info("telegram: starting")

	bot, err := telego.NewBot(c.token, telego.WithDiscardLogger())
	if err != nil {
		return fmt.Errorf("telegram: new bot: %w", err)
	}
	c.bot = bot
	c.send = bot

	me, err := bot.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram: get me: %w", err)
	}
	c.username = me.Username
	slog.Info("telegram: connected", "bot", me.Username)

	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel = cancel

	updates, err := bot.UpdatesViaLongPolling(runCtx, nil)
	if err != nil {
		cancel()
		return fmt.Errorf("telegram: long polling: %w", err)
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for update := range updates {
			c.handleUpdate(runCtx, update)
		}
	}()

	return nil
}

// handleUpdate filters one incoming update and dispatches accepted text
// messages to the handler.
func (c *Connector) handleUpdate(ctx context.Context, update telego.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.Text == "" {
		return
	}

	userID := strconv.FormatInt(msg.From.ID, 10)
	if !c.isAllowed(userID) {
		slog.Info("telegram: blocked (not in allow list)", "user", userID)
		return
	}

	text, ok := c.triggerText(msg)
	if !ok {
		return // group message that didn't trigger the bot
	}

	if c.handler == nil {
		return
	}
	slog.Info("telegram: msg", "from", userID, "chat", msg.Chat.ID, "text", truncate(text, 60))
	c.handler(ctx, connector.Message{
		Connector: connectorName,
		UserID:    userID,
		ChatID:    strconv.FormatInt(msg.Chat.ID, 10),
		SenderID:  userID,
		Text:      text,
	})
}

// triggerText decides whether a message should be handled and returns the text
// to process (with a leading @mention stripped). Private chats always trigger.
// In groups, when group_trigger.mention_only is set, the bot only responds when
// @-mentioned or when the text starts with a configured prefix.
func (c *Connector) triggerText(msg *telego.Message) (string, bool) {
	text := msg.Text
	if msg.Chat.Type == "private" {
		return text, true
	}

	gt := c.configFn().Connectors.Telegram.GroupTrigger
	if !gt.MentionOnly {
		return text, true
	}

	if c.username != "" {
		mention := "@" + c.username
		if strings.Contains(text, mention) {
			return strings.TrimSpace(strings.ReplaceAll(text, mention, "")), true
		}
	}
	for _, p := range gt.Prefixes {
		if p != "" && strings.HasPrefix(text, p) {
			return strings.TrimSpace(strings.TrimPrefix(text, p)), true
		}
	}
	return "", false
}

// Send delivers text to a chat, splitting it into <=maxMessageLen chunks.
func (c *Connector) Send(ctx context.Context, chatID, text string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat id %q: %w", chatID, err)
	}
	for _, chunk := range splitMessage(text, maxMessageLen) {
		_, err := c.send.SendMessage(ctx, &telego.SendMessageParams{
			ChatID: telego.ChatID{ID: id},
			Text:   chunk,
		})
		if err != nil {
			return fmt.Errorf("telegram: send: %w", err)
		}
	}
	return nil
}

func (c *Connector) Stop(_ context.Context) error {
	slog.Info("telegram: stopping")
	if c.runCancel != nil {
		c.runCancel()
	}
	c.wg.Wait()
	return nil
}

// splitMessage breaks text into chunks of at most limit runes, preferring to
// split on a newline within the last 20% of a chunk so messages stay readable.
// An empty string yields a single empty chunk so callers always send something.
func splitMessage(text string, limit int) []string {
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	var chunks []string
	for len(runes) > limit {
		cut := limit
		// Prefer a newline boundary in the last fifth of the window.
		if nl := lastIndexRune(runes[:limit], '\n', limit*4/5); nl > 0 {
			cut = nl + 1
		}
		chunks = append(chunks, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

// lastIndexRune returns the index of the last r in s at or after min, or -1.
func lastIndexRune(s []rune, r rune, min int) int {
	for i := len(s) - 1; i >= min; i-- {
		if s[i] == r {
			return i
		}
	}
	return -1
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
