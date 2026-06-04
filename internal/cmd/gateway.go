package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/connector/multi"
	"github.com/sho0pi/god/internal/connector/socket"
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

	return children, nil
}

// resolveWhatsAppStore returns the WhatsApp session store directory. If
// configured is non-empty it is used as-is. Otherwise it defaults to
// ~/.god/whatsapp, and any existing data/whatsapp directory from the old
// default is migrated there automatically on first run.
func resolveWhatsAppStore(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	target, err := godhome.Path("whatsapp")
	if err != nil {
		return "", err
	}
	migrateWhatsAppStore("data/whatsapp", target)
	return target, nil
}

// migrateWhatsAppStore moves src to dst when src exists and dst does not.
func migrateWhatsAppStore(src, dst string) {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return
	}
	if _, err := os.Stat(dst); err == nil {
		return // destination already present — nothing to do
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		slog.Warn("whatsapp: migration: cannot create parent dir", "err", err)
		return
	}
	if err := os.Rename(src, dst); err != nil {
		slog.Warn("whatsapp: migration: move failed — move manually", "src", src, "dst", dst, "err", err)
		return
	}
	slog.Info("whatsapp: migrated session store", "from", src, "to", dst)
}

func init() {
	gatewayCmd.AddCommand(gatewayStartCmd)
	rootCmd.AddCommand(gatewayCmd)
}
