package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/connector/whatsapp"
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

		conn := whatsapp.New(storePath, loader.Supplier())
		runAgent(ctx, conn)
		return nil
	},
}

func init() {
	whatsappCmd.Flags().String("store", "", "path to WhatsApp session storage (overrides config)")
	_ = whatsappCmd.Flags().MarkHidden("store")
	rootCmd.AddCommand(whatsappCmd)
}
