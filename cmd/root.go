package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/config"
)

var (
	cfgFile string
	loader  *config.Loader
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "god",
	Short: "God — a minimal extensible AI agent",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		loader, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		cfg = loader.Cfg
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", config.DefaultPath, "config file path")
}
