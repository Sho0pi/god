package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	cliconn "github.com/sho0pi/god/connector/cli"
)

var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "Start the CLI connector (chat in terminal)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Connectors.CLI.Enabled {
			return fmt.Errorf("cli connector is disabled in config")
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		runAgent(ctx, cliconn.New())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cliCmd)
}
