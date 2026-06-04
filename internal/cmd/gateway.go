package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/connector/multi"
	"github.com/sho0pi/god/internal/connector/socket"
	"github.com/sho0pi/god/internal/connector/telegram"
	"github.com/sho0pi/god/internal/connector/whatsapp"
	"github.com/sho0pi/god/internal/godhome"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Run god as a long-lived gateway",
}

var gatewayStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the gateway: serve enabled connectors and the control socket",
	Long: `Start runs one agent process behind every enabled connector at once
(WhatsApp, plus the control socket that "god cli" connects to). All front-ends
share the same agent, LLM pool, store, and memory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		release, err := godhome.AcquireGatewayLock()
		if err != nil {
			return err
		}
		defer release()

		a := appFrom(cmd)

		children, err := buildGatewayConnectors(a)
		if err != nil {
			return err
		}
		if len(children) == 0 {
			return fmt.Errorf("no connectors enabled — enable at least the cli or whatsapp connector in config")
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		a.runAgent(ctx, multi.New(children...))
		return nil
	},
}

// buildGatewayConnectors assembles the child connectors the gateway should run,
// based on config. The control socket stands in for the "cli" connector.
func buildGatewayConnectors(a *app) ([]connector.Connector, error) {
	var children []connector.Connector

	if a.cfg.Connectors.CLI.Enabled {
		sockPath, err := godhome.SocketPath()
		if err != nil {
			return nil, fmt.Errorf("socket path: %w", err)
		}
		children = append(children, socket.NewServer(sockPath))
		slog.Info("gateway: cli control socket", "path", sockPath)
	}

	if a.cfg.Connectors.WhatsApp.Enabled {
		storePath, err := resolveWhatsAppStore(a.cfg.Connectors.WhatsApp.StorePath)
		if err != nil {
			return nil, fmt.Errorf("whatsapp store path: %w", err)
		}
		children = append(children, whatsapp.New(storePath, a.loader.Supplier()))
		slog.Info("gateway: whatsapp connector", "store", storePath)
	}

	if a.cfg.Connectors.Telegram.Enabled {
		token := a.cfg.Connectors.Telegram.Token
		if token == "" {
			token = os.Getenv("TELEGRAM_BOT_TOKEN")
		}
		if token == "" {
			return nil, fmt.Errorf("telegram enabled but no token — set connectors.telegram.token or TELEGRAM_BOT_TOKEN")
		}
		children = append(children, telegram.New(token, a.loader.Supplier()))
		slog.Info("gateway: telegram connector")
	}

	return children, nil
}

// resolveWhatsAppStore returns the WhatsApp session store directory.
// If configured is non-empty it is used as-is; otherwise defaults to ~/.god/whatsapp.
func resolveWhatsAppStore(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return godhome.Path("whatsapp")
}

func init() {
	gatewayCmd.AddCommand(gatewayStartCmd)
	rootCmd.AddCommand(gatewayCmd)
}
