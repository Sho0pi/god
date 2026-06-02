package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/connector/whatsapp"
)

var whatsappCmd = &cobra.Command{
	Use:   "whatsapp",
	Short: "Start the WhatsApp connector",
	RunE: func(cmd *cobra.Command, args []string) error {
		storePath, _ := cmd.Flags().GetString("store")

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		runAgent(ctx, whatsapp.New(storePath))
		return nil
	},
}

func init() {
	storePath := os.Getenv("WHATSAPP_STORE_PATH")
	if storePath == "" {
		storePath = "data/whatsapp"
	}
	whatsappCmd.Flags().String("store", storePath, "path to WhatsApp session storage")
	rootCmd.AddCommand(whatsappCmd)
}
