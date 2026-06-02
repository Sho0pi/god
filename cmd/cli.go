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
	Short: "Start the CLI connector (interactive or one-shot with --msg)",
	Example: `  god cli                              # interactive chat
  god cli --msg "hello"                # send one message, print reply, exit
  god cli --msg "hi" --user alice      # send as user 'alice'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Connectors.CLI.Enabled {
			return fmt.Errorf("cli connector is disabled in config")
		}

		msg, _ := cmd.Flags().GetString("msg")
		userID, _ := cmd.Flags().GetString("user")

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		conn := cliconn.New(cliconn.Options{
			UserID:  userID,
			Message: msg,
			OnDone:  stop, // one-shot: cancel signal ctx after reply received
		})

		runAgent(ctx, conn)
		return nil
	},
}

func init() {
	cliCmd.Flags().StringP("msg", "m", "", "send a single message and exit (non-interactive)")
	cliCmd.Flags().StringP("user", "u", "local", "userID to send as (creates user if not exists)")
	rootCmd.AddCommand(cliCmd)
}
