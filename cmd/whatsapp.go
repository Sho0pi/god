package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/config"
	"github.com/sho0pi/god/connector/whatsapp"
)

var whatsappCmd = &cobra.Command{
	Use:   "whatsapp",
	Short: "Start the WhatsApp connector",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Connectors.WhatsApp.Enabled {
			return fmt.Errorf("whatsapp connector is disabled in config")
		}

		storePath, _ := cmd.Flags().GetString("store")
		if storePath == "" {
			storePath = cfg.Connectors.WhatsApp.StorePath
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		waConnector := whatsapp.New(storePath, cfg.Connectors.WhatsApp.Allow, cfg.Connectors.WhatsApp.GroupTrigger)

		loader.Watch(func(newCfg *config.Config) {
			waConnector.Reload(newCfg.Connectors.WhatsApp.Allow, newCfg.Connectors.WhatsApp.GroupTrigger)
		})

		runAgent(ctx, waConnector)
		return nil
	},
}

func init() {
	whatsappCmd.Flags().String("store", "", "path to WhatsApp session storage (overrides config)")
	_ = whatsappCmd.Flags().MarkHidden("store")
	rootCmd.AddCommand(whatsappCmd)
}
