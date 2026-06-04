package cmd

import (
	"testing"

	"github.com/sho0pi/god/internal/config"
)

func TestCheckTelegram(t *testing.T) {
	t.Run("disabled passes", func(t *testing.T) {
		cfg := &config.Config{}
		if c := checkTelegram(cfg); !c.ok {
			t.Errorf("disabled telegram should pass, got %q", c.hint)
		}
	})

	t.Run("enabled without token fails", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "")
		cfg := &config.Config{}
		cfg.Connectors.Telegram.Enabled = true
		if c := checkTelegram(cfg); c.ok {
			t.Error("enabled telegram with no token should fail")
		}
	})

	t.Run("enabled with config token passes", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Connectors.Telegram.Enabled = true
		cfg.Connectors.Telegram.Token = "123:ABC"
		if c := checkTelegram(cfg); !c.ok {
			t.Errorf("token set should pass, got %q", c.hint)
		}
	})

	t.Run("enabled with env token passes", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "123:ABC")
		cfg := &config.Config{}
		cfg.Connectors.Telegram.Enabled = true
		if c := checkTelegram(cfg); !c.ok {
			t.Errorf("env token should pass, got %q", c.hint)
		}
	})
}

func TestCheckGeminiKey(t *testing.T) {
	t.Run("set passes", func(t *testing.T) {
		t.Setenv("GEMINI_API_KEY", "x")
		if c := checkGeminiKey(); !c.ok {
			t.Error("key set should pass")
		}
	})
	t.Run("missing fails", func(t *testing.T) {
		t.Setenv("GEMINI_API_KEY", "")
		if c := checkGeminiKey(); c.ok {
			t.Error("missing key should fail")
		}
	})
}
