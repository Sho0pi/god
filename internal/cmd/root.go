package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/godhome"
)

// app holds the per-invocation dependencies, replacing package-level globals.
// It is built in PersistentPreRunE and carried through cobra's command context.
type app struct {
	loader  *config.Loader
	cfg     *config.Config
	cfgFile string
}

type appKey struct{}

// appFrom retrieves the app from the command context. Panics if missing, which
// only happens if a command runs without the PersistentPreRunE that sets it.
func appFrom(cmd *cobra.Command) *app {
	return cmd.Context().Value(appKey{}).(*app)
}

// withApp stores an app in the command context for downstream handlers.
func withApp(cmd *cobra.Command, a *app) {
	cmd.SetContext(context.WithValue(cmd.Context(), appKey{}, a))
}

var rootCmd = &cobra.Command{
	Use:   "god",
	Short: "God — a minimal extensible AI agent",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		if path == "" {
			// No explicit --config: default to ~/.god/god.yaml.
			p, err := godhome.Path("god.yaml")
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			path = p
		}
		loader, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		withApp(cmd, &app{loader: loader, cfg: loader.Cfg, cfgFile: path})
		return nil
	},
}

func Execute() {
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Default shown in --help; empty fallback lets PersistentPreRunE resolve
	// ~/.god/god.yaml if the home dir can't be determined at init time.
	def, _ := godhome.Path("god.yaml")
	rootCmd.PersistentFlags().String("config", def, "config file path")
}
