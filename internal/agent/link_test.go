package agent

import (
	"context"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/llm"
	"github.com/sho0pi/god/internal/store"
)

// linkStore is a no-op store.Store; only Link is exercised by these tests.
type linkStore struct{}

func (linkStore) AssignSoul(context.Context, string, string, string) error            { return nil }
func (linkStore) GetSoul(context.Context, string, string) (string, error)             { return "", nil }
func (linkStore) DeleteSoul(context.Context, string, string) error                    { return nil }
func (linkStore) AssignRole(context.Context, string, string, string) error            { return nil }
func (linkStore) GetRole(context.Context, string, string) (string, error)             { return "", nil }
func (linkStore) DeleteRole(context.Context, string, string) error                    { return nil }
func (linkStore) SaveMemory(context.Context, string, string, string, []float32) error { return nil }
func (linkStore) SearchMemories(context.Context, string, string, []float32, int) ([]string, error) {
	return nil, nil
}
func (linkStore) DeleteMemories(context.Context, string, string) error { return nil }
func (linkStore) AddAllow(context.Context, string, string) error       { return nil }
func (linkStore) RemoveAllow(context.Context, string, string) error    { return nil }
func (linkStore) ListAllow(context.Context, string) ([]string, error)  { return nil, nil }
func (linkStore) ResolveIdentity(_ context.Context, c, u string) (string, string, error) {
	return c, u, nil
}
func (linkStore) Link(context.Context, string, string, string, string) error  { return nil }
func (linkStore) Unlink(context.Context, string, string) error                { return nil }
func (linkStore) SaveReminder(context.Context, store.Reminder) (int64, error) { return 0, nil }
func (linkStore) ListEnabledReminders(context.Context) ([]store.Reminder, error) {
	return nil, nil
}
func (linkStore) ListReminders(context.Context, string, string) ([]store.Reminder, error) {
	return nil, nil
}
func (linkStore) DeleteReminder(context.Context, string, string, int64) (bool, error) {
	return false, nil
}
func (linkStore) Close() error { return nil }

func newLinkTestAgent() *Agent {
	return &Agent{
		store:     linkStore{},
		linkCodes: make(map[string]linkCode),
		history:   make(map[string][]llm.Message),
		timers:    make(map[string]*time.Timer),
	}
}

func TestLinkCodeGenerateAndRedeem(t *testing.T) {
	a := newLinkTestAgent()
	code := a.generateLinkCode("whatsapp", "972")
	if code == "" {
		t.Fatal("empty code")
	}
	label, err := a.redeemLinkCode(context.Background(), code, "telegram", "7474")
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if label != "whatsapp:972" {
		t.Errorf("label = %q, want whatsapp:972", label)
	}
	// Codes are one-time — a second redeem fails.
	if _, err := a.redeemLinkCode(context.Background(), code, "telegram", "7474"); err == nil {
		t.Error("code should be single-use")
	}
}

func TestRedeemUnknownCode(t *testing.T) {
	a := newLinkTestAgent()
	if _, err := a.redeemLinkCode(context.Background(), "nope", "telegram", "1"); err == nil {
		t.Error("unknown code should error")
	}
}

func TestRedeemExpiredCode(t *testing.T) {
	a := newLinkTestAgent()
	a.linkCodes["OLD"] = linkCode{connector: "whatsapp", userID: "972", expires: time.Now().Add(-time.Minute)}
	if _, err := a.redeemLinkCode(context.Background(), "OLD", "telegram", "1"); err == nil {
		t.Error("expired code should error")
	}
}

// mapStore resolves a fixed satellite→hub mapping; everything else is self/no-op.
type mapStore struct {
	linkStore
	from, toConn, toUser string
}

func (m mapStore) ResolveIdentity(_ context.Context, c, u string) (string, string, error) {
	if c+":"+u == m.from {
		return m.toConn, m.toUser, nil
	}
	return c, u, nil
}

// A linked satellite must inherit admin from the bootstrap list, which matches
// the canonical (hub) id — the bug where Telegram stayed "user" after linking.
func TestResolveRoleInheritsAdminViaLink(t *testing.T) {
	a := &Agent{
		store: mapStore{from: "telegram:7474", toConn: "whatsapp", toUser: "972"},
		configFn: func() *config.Config {
			return &config.Config{Admin: []string{"972"}}
		},
	}
	got := a.resolveRole(context.Background(), connector.Message{Connector: "telegram", UserID: "7474"})
	if got != "admin" {
		t.Errorf("linked identity role = %q, want admin (inherited from hub)", got)
	}

	// An unlinked, non-admin telegram id stays a regular user.
	got = a.resolveRole(context.Background(), connector.Message{Connector: "telegram", UserID: "5555"})
	if got != "user" {
		t.Errorf("unlinked non-admin role = %q, want user", got)
	}
}
