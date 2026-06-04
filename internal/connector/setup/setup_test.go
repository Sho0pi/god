package setup

import (
	"context"
	"testing"

	"github.com/sho0pi/god/internal/config"
)

func TestConnectorsRegistered(t *testing.T) {
	for _, key := range []string{"telegram", "whatsapp"} {
		if _, ok := Lookup(key); !ok {
			t.Errorf("%q wizard not registered", key)
		}
	}
	if len(All()) < 2 {
		t.Errorf("expected >=2 wizards, got %d", len(All()))
	}
}

func TestTelegramStatus(t *testing.T) {
	w, _ := Lookup("telegram")

	off := &config.Config{}
	if w.Enabled(off) {
		t.Error("telegram should be disabled by default")
	}
	if exists, _ := w.SessionStatus(off); exists {
		t.Error("no token → SessionStatus should report not configured")
	}

	on := &config.Config{}
	on.Connectors.Telegram.Enabled = true
	on.Connectors.Telegram.Token = "123:ABC"
	if !w.Enabled(on) {
		t.Error("telegram should be enabled")
	}
	if exists, _ := w.SessionStatus(on); !exists {
		t.Error("token set → SessionStatus should report configured")
	}
}

// telegramWizard.Setup keeps the existing token (no network) when not resetting.
func TestTelegramSetupKeep(t *testing.T) {
	w, _ := Lookup("telegram")
	cfg := &config.Config{}
	cfg.Connectors.Telegram.Token = "123:ABC"

	edits, err := w.Setup(context.Background(), cfg, false)
	if err != nil {
		t.Fatalf("Setup keep: %v", err)
	}
	if edits["connectors.telegram.enabled"] != true {
		t.Errorf("keep should enable, got %v", edits)
	}
	if _, hasToken := edits["connectors.telegram.token"]; hasToken {
		t.Error("keep should not rewrite the token")
	}
}
