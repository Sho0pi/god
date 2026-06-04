package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/godhome"
	"github.com/sho0pi/god/internal/llm/setup"
)

// customModel is the sentinel option value for "type a model id myself".
const customModel = "\x00custom"

// isConfigured reports whether the provider's API key is present in the
// environment (main loads ~/.god/.env before commands run).
func isConfigured(p setup.Provider) bool {
	return os.Getenv(p.EnvVar()) != ""
}

var modelCmd = &cobra.Command{
	Use:   "model [provider]",
	Short: "Set up LLM providers and choose the default model",
	Long: `model configures god's LLM providers.

Run it with no arguments for an interactive menu of every provider and its
status, or name one (e.g. "god model openai") to set it up directly. It saves
the API key to ~/.god/.env and can set the global default provider+model.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		a := appFrom(cmd)
		p, err := pickProvider(a.cfg)
		if err != nil || p == nil {
			return err
		}
		return runProvider(cmd.Context(), a, p)
	},
}

// pickProvider shows the menu and returns the chosen provider (nil if cancelled).
func pickProvider(cfg *config.Config) (setup.Provider, error) {
	providers := setup.All()
	opts := make([]huh.Option[string], 0, len(providers))
	for _, p := range providers {
		status := "no key"
		if isConfigured(p) {
			status = "key set"
		}
		marker := ""
		if cfg.LLM.Provider == p.Key() || (cfg.LLM.Provider == "" && p.Key() == "gemini") {
			marker = "  (default)"
		}
		label := fmt.Sprintf("%-10s [%s]%s", p.Title(), status, marker)
		opts = append(opts, huh.NewOption(label, p.Key()))
	}

	var choice string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Which provider do you want to set up?").
			Options(opts...).
			Value(&choice),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	p, _ := setup.Lookup(choice)
	return p, nil
}

// runProvider configures one provider: set/replace its API key, then optionally
// make it the global default model.
func runProvider(ctx context.Context, a *app, p setup.Provider) error {
	// 1. API key — prompt unless one is already set and the user keeps it.
	if err := configureKey(ctx, p); err != nil {
		return err
	}

	// 2. Optionally set as the global default provider+model.
	setDefault := true
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Make %s the default model?", p.Title())).
			Value(&setDefault),
	)).Run(); err != nil {
		return err
	}
	if !setDefault {
		fmt.Printf("%s key saved to ~/.god/.env.\n", p.Title())
		return nil
	}

	model, err := pickModel(p)
	if err != nil {
		return err
	}
	if err := config.SetValues(a.cfgFile, map[string]any{
		"llm.provider": p.Key(),
		"llm.model":    model,
	}); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Default model set to %s/%s. Restart `god gateway start` to apply.\n", p.Key(), model)
	return nil
}

// configureKey prompts for and validates an API key, then writes it to the env
// file. If a key is already set it offers to keep it.
func configureKey(ctx context.Context, p setup.Provider) error {
	if isConfigured(p) {
		keep := true
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("%s already has an API key (%s). Keep it?", p.Title(), p.EnvVar())).
				Value(&keep),
		)).Run(); err != nil {
			return err
		}
		if keep {
			return nil
		}
	}

	var key string
	input := huh.NewInput().
		Title(fmt.Sprintf("%s API key", p.Title())).
		Description(fmt.Sprintf("Stored in ~/.god/.env as %s.", p.EnvVar())).
		EchoMode(huh.EchoModePassword).
		Value(&key).
		Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return fmt.Errorf("key is required")
			}
			vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			return p.ValidateKey(vctx, s)
		})
	if err := huh.NewForm(huh.NewGroup(input)).Run(); err != nil {
		return err
	}

	if err := godhome.SetEnv(p.EnvVar(), strings.TrimSpace(key)); err != nil {
		return fmt.Errorf("save key: %w", err)
	}
	fmt.Printf("✓ %s key valid and saved\n", p.Title())
	return nil
}

// pickModel lets the user choose a curated model or enter a custom id.
func pickModel(p setup.Provider) (string, error) {
	opts := make([]huh.Option[string], 0, len(p.Models())+1)
	for _, m := range p.Models() {
		opts = append(opts, huh.NewOption(m, m))
	}
	opts = append(opts, huh.NewOption("Other (type a model id)…", customModel))

	var choice string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Default model").Options(opts...).Value(&choice),
	)).Run(); err != nil {
		return "", err
	}
	if choice != customModel {
		return choice, nil
	}

	var custom string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Model id").Value(&custom).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("model id is required")
			}
			return nil
		}),
	)).Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(custom), nil
}

func init() {
	// One subcommand per provider, so `god model openai` works and cobra
	// autocompletes provider names with descriptions.
	for _, p := range setup.All() {
		p := p
		modelCmd.AddCommand(&cobra.Command{
			Use:   p.Key(),
			Short: "Set up the " + p.Title() + " provider",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runProvider(cmd.Context(), appFrom(cmd), p)
			},
		})
	}
	rootCmd.AddCommand(modelCmd)
}
