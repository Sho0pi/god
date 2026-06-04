package setup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector/telegram"
)

func init() { Register(telegramWizard{}) }

type telegramWizard struct{}

func (telegramWizard) Key() string   { return "telegram" }
func (telegramWizard) Title() string { return "Telegram" }

func (telegramWizard) Enabled(cfg *config.Config) bool {
	return cfg.Connectors.Telegram.Enabled
}

func (telegramWizard) SessionStatus(cfg *config.Config) (bool, string) {
	if cfg.Connectors.Telegram.Token != "" {
		return true, "token set"
	}
	return false, "no token"
}

func (telegramWizard) Setup(ctx context.Context, cfg *config.Config, reset bool) (map[string]any, error) {
	// Keep the existing token: just make sure the connector is enabled.
	if !reset && cfg.Connectors.Telegram.Token != "" {
		return map[string]any{"connectors.telegram.enabled": true}, nil
	}

	var (
		token    string
		username string
	)

	intro := huh.NewNote().
		Title("Create a Telegram bot").
		Description("1. Open Telegram and message @BotFather\n" +
			"2. Send /newbot and follow the prompts (name, then a username ending in 'bot')\n" +
			"3. BotFather replies with a token like 123456:ABC-DEF…\n" +
			"4. Paste that token below.")

	input := huh.NewInput().
		Title("Bot token").
		Placeholder("123456789:ABCdef…").
		EchoMode(huh.EchoModePassword).
		Value(&token).
		Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return fmt.Errorf("token is required")
			}
			vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			name, err := telegram.Validate(vctx, s)
			if err != nil {
				return err
			}
			username = name
			return nil
		})

	form := huh.NewForm(huh.NewGroup(intro, input))
	if err := form.Run(); err != nil {
		return nil, err
	}

	fmt.Printf("✓ connected to @%s\n", username)
	return map[string]any{
		"connectors.telegram.enabled": true,
		"connectors.telegram.token":   strings.TrimSpace(token),
	}, nil
}
