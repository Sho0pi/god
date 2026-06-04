package telegram

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mymmrac/telego"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
)

// cfgFn builds a config supplier with the given Telegram settings.
func cfgFn(tg config.TelegramConfig) func() *config.Config {
	c := &config.Config{}
	c.Connectors.Telegram = tg
	return func() *config.Config { return c }
}

func TestIsAllowed(t *testing.T) {
	t.Run("empty list accepts all", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{})}
		if !c.isAllowed("123456") {
			t.Error("empty allow list should accept everyone")
		}
	})

	t.Run("listed id gates", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{Allow: []string{"123", "456"}})}
		if !c.isAllowed("456") {
			t.Error("listed id should be allowed")
		}
		if c.isAllowed("789") {
			t.Error("unlisted id should be blocked once list is non-empty")
		}
	})

	t.Run("merges store source", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{Allow: []string{"123"}})}
		c.allowSource = func() []string { return []string{"999"} }
		if !c.isAllowed("999") {
			t.Error("store-backed id should be allowed")
		}
		if !c.isAllowed("123") {
			t.Error("yaml id should still be allowed")
		}
		if c.isAllowed("555") {
			t.Error("id in neither source should be blocked")
		}
	})

	t.Run("trims whitespace in config", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{Allow: []string{" 123 "}})}
		if !c.isAllowed("123") {
			t.Error("whitespace around configured id should be ignored")
		}
	})
}

func TestSplitMessage(t *testing.T) {
	t.Run("short text is one chunk", func(t *testing.T) {
		got := splitMessage("hello", 4096)
		if len(got) != 1 || got[0] != "hello" {
			t.Fatalf("got %v, want [hello]", got)
		}
	})

	t.Run("empty text yields one empty chunk", func(t *testing.T) {
		got := splitMessage("", 4096)
		if len(got) != 1 || got[0] != "" {
			t.Fatalf("got %v, want one empty chunk", got)
		}
	})

	t.Run("over limit splits into multiple", func(t *testing.T) {
		text := strings.Repeat("a", 10000)
		got := splitMessage(text, 4096)
		if len(got) != 3 {
			t.Fatalf("got %d chunks, want 3", len(got))
		}
		// No chunk exceeds the limit; concatenation is lossless.
		var sb strings.Builder
		for _, c := range got {
			if n := len([]rune(c)); n > 4096 {
				t.Errorf("chunk len %d exceeds limit", n)
			}
			sb.WriteString(c)
		}
		if sb.String() != text {
			t.Error("rejoined chunks must equal original")
		}
	})

	t.Run("exact boundary is one chunk", func(t *testing.T) {
		text := strings.Repeat("x", 4096)
		if got := splitMessage(text, 4096); len(got) != 1 {
			t.Fatalf("got %d chunks, want 1", len(got))
		}
	})

	t.Run("prefers newline boundary", func(t *testing.T) {
		// 90 runes, a newline near the end of the window, limit 100 so the
		// whole thing fits — but force a split with a smaller limit.
		head := strings.Repeat("a", 80) + "\n"
		tail := strings.Repeat("b", 50)
		got := splitMessage(head+tail, 90)
		if len(got) != 2 {
			t.Fatalf("got %d chunks, want 2", len(got))
		}
		if !strings.HasSuffix(got[0], "\n") {
			t.Errorf("first chunk should end at newline, got %q", got[0])
		}
	})

	t.Run("multibyte runes are not cut mid-character", func(t *testing.T) {
		text := strings.Repeat("é", 5000) // 2 bytes each, 1 rune each
		got := splitMessage(text, 4096)
		for _, c := range got {
			if !utf8.ValidString(c) {
				t.Error("chunk contains invalid UTF-8 (rune was split)")
			}
		}
	})
}

// fakeSender records the SendMessage calls made to it.
type fakeSender struct {
	calls []*telego.SendMessageParams
	err   error
}

func (f *fakeSender) SendMessage(_ context.Context, p *telego.SendMessageParams) (*telego.Message, error) {
	f.calls = append(f.calls, p)
	return &telego.Message{}, f.err
}

func TestSendChunking(t *testing.T) {
	fs := &fakeSender{}
	c := &Connector{configFn: cfgFn(config.TelegramConfig{}), send: fs}

	long := strings.Repeat("a", 9000) // → 3 chunks at 4096
	if err := c.Send(context.Background(), "42", long); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(fs.calls) != 3 {
		t.Fatalf("got %d send calls, want 3", len(fs.calls))
	}
	for _, call := range fs.calls {
		if call.ChatID.ID != 42 {
			t.Errorf("chat id = %d, want 42", call.ChatID.ID)
		}
	}
}

func TestSendRejectsBadChatID(t *testing.T) {
	c := &Connector{configFn: cfgFn(config.TelegramConfig{}), send: &fakeSender{}}
	if err := c.Send(context.Background(), "not-a-number", "hi"); err == nil {
		t.Error("expected error for non-numeric chat id")
	}
}

func TestHandleUpdateDispatch(t *testing.T) {
	update := func(userID int64, chatType, text string) telego.Update {
		return telego.Update{Message: &telego.Message{
			Text: text,
			From: &telego.User{ID: userID},
			Chat: telego.Chat{ID: 99, Type: chatType},
		}}
	}

	t.Run("allowed private message reaches handler", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{})}
		var got *connector.Message
		c.SetMessageHandler(func(_ context.Context, m connector.Message) { got = &m })
		c.handleUpdate(context.Background(), update(123, "private", "hello"))
		if got == nil {
			t.Fatal("handler not called")
		}
		if got.UserID != "123" || got.ChatID != "99" || got.Text != "hello" || got.Connector != connectorName {
			t.Errorf("bad message: %+v", *got)
		}
	})

	t.Run("blocked user is dropped", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{Allow: []string{"999"}})}
		called := false
		c.SetMessageHandler(func(context.Context, connector.Message) { called = true })
		c.handleUpdate(context.Background(), update(123, "private", "hi"))
		if called {
			t.Error("blocked user should not reach handler")
		}
	})

	t.Run("non-text update is ignored", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{})}
		called := false
		c.SetMessageHandler(func(context.Context, connector.Message) { called = true })
		c.handleUpdate(context.Background(), telego.Update{})          // no Message
		c.handleUpdate(context.Background(), update(1, "private", "")) // empty text
		if called {
			t.Error("non-text updates should be ignored")
		}
	})

	t.Run("group without mention is dropped", func(t *testing.T) {
		c := &Connector{
			configFn: cfgFn(config.TelegramConfig{GroupTrigger: config.GroupTriggerConfig{MentionOnly: true}}),
			username: "godbot",
		}
		called := false
		c.SetMessageHandler(func(context.Context, connector.Message) { called = true })
		c.handleUpdate(context.Background(), update(1, "group", "no mention here"))
		if called {
			t.Error("un-mentioned group message should be dropped")
		}
	})
}

func TestValidateRejectsMalformedToken(t *testing.T) {
	// telego validates token format in NewBot, so this fails fast without a
	// network call.
	if _, err := Validate(context.Background(), "not-a-valid-token"); err == nil {
		t.Error("expected error for malformed token")
	}
}

func TestTriggerText(t *testing.T) {
	priv := func(text string) *telego.Message {
		return &telego.Message{Text: text, Chat: telego.Chat{Type: "private"}}
	}
	group := func(text string) *telego.Message {
		return &telego.Message{Text: text, Chat: telego.Chat{Type: "group"}}
	}

	t.Run("private always triggers", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{
			GroupTrigger: config.GroupTriggerConfig{MentionOnly: true},
		})}
		got, ok := c.triggerText(priv("hello"))
		if !ok || got != "hello" {
			t.Errorf("got (%q, %v), want (hello, true)", got, ok)
		}
	})

	t.Run("group without mention_only triggers", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{})}
		if _, ok := c.triggerText(group("hi all")); !ok {
			t.Error("group should trigger when mention_only is false")
		}
	})

	t.Run("group with mention_only requires mention", func(t *testing.T) {
		c := &Connector{
			configFn: cfgFn(config.TelegramConfig{GroupTrigger: config.GroupTriggerConfig{MentionOnly: true}}),
			username: "godbot",
		}
		if _, ok := c.triggerText(group("nothing for me")); ok {
			t.Error("group message without mention should not trigger")
		}
		got, ok := c.triggerText(group("@godbot what time is it"))
		if !ok {
			t.Fatal("mention should trigger")
		}
		if strings.Contains(got, "@godbot") {
			t.Errorf("mention should be stripped, got %q", got)
		}
		if got != "what time is it" {
			t.Errorf("got %q, want stripped text", got)
		}
	})

	t.Run("prefix triggers in group", func(t *testing.T) {
		c := &Connector{configFn: cfgFn(config.TelegramConfig{
			GroupTrigger: config.GroupTriggerConfig{MentionOnly: true, Prefixes: []string{"!god"}},
		})}
		got, ok := c.triggerText(group("!god hello"))
		if !ok || got != "hello" {
			t.Errorf("got (%q, %v), want (hello, true)", got, ok)
		}
	})
}
