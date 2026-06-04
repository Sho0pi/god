package cmd

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector/setup"
)

var connectorCmd = &cobra.Command{
	Use:   "connector [name]",
	Short: "Set up and configure connectors (WhatsApp, Telegram, …)",
	Long: `connector configures god's chat connectors.

Run it with no arguments for an interactive menu of every connector and its
status, or name one (e.g. "god connector telegram") to set it up directly.`,
	// Bare `god connector` → interactive menu over all connectors.
	RunE: func(cmd *cobra.Command, args []string) error {
		a := appFrom(cmd)
		w, err := pickConnector(a.cfg)
		if err != nil || w == nil {
			return err
		}
		return runWizard(cmd.Context(), a, w)
	},
}

// pickConnector shows the menu and returns the chosen wizard (nil if cancelled).
func pickConnector(cfg *config.Config) (setup.Wizard, error) {
	wizards := setup.All()
	opts := make([]huh.Option[string], 0, len(wizards))
	for _, w := range wizards {
		enabled := "disabled"
		if w.Enabled(cfg) {
			enabled = "enabled"
		}
		_, detail := w.SessionStatus(cfg)
		label := fmt.Sprintf("%-10s [%s]  %s", w.Title(), enabled, detail)
		opts = append(opts, huh.NewOption(label, w.Key()))
	}

	var choice string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Which connector do you want to set up?").
			Options(opts...).
			Value(&choice),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	w, _ := setup.Lookup(choice)
	return w, nil
}

// runWizard handles the keep/reset prompt for an existing session, runs the
// wizard, and persists the resulting config edits.
func runWizard(ctx context.Context, a *app, w setup.Wizard) error {
	reset := false
	if exists, detail := w.SessionStatus(a.cfg); exists {
		var action string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("%s is already set up (%s). Keep it or reset?", w.Title(), detail)).
				Options(
					huh.NewOption("Keep the existing session", "keep"),
					huh.NewOption("Reset and set up again", "reset"),
				).
				Value(&action),
		))
		if err := form.Run(); err != nil {
			return err
		}
		reset = action == "reset"
	}

	edits, err := w.Setup(ctx, a.cfg, reset)
	if err != nil {
		return err
	}
	if len(edits) == 0 {
		return nil
	}
	if err := config.SetValues(a.cfgFile, edits); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Saved to %s. Start or restart `god gateway start` to apply.\n", a.cfgFile)
	return nil
}

func init() {
	// One subcommand per registered connector, so `god connector telegram` works
	// and cobra autocompletes connector names with descriptions.
	for _, w := range setup.All() {
		w := w
		connectorCmd.AddCommand(&cobra.Command{
			Use:   w.Key(),
			Short: "Set up the " + w.Title() + " connector",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runWizard(cmd.Context(), appFrom(cmd), w)
			},
		})
	}
	rootCmd.AddCommand(connectorCmd)
}
