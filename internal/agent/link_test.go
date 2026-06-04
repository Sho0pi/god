package agent

import (
	"context"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/llm"
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
func (linkStore) Link(context.Context, string, string, string, string) error { return nil }
func (linkStore) Unlink(context.Context, string, string) error               { return nil }
func (linkStore) Close() error                                               { return nil }

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
