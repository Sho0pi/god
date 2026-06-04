package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
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
		log.Printf("gateway: cli control socket at %s", sockPath)
	}

	if a.cfg.Connectors.WhatsApp.Enabled {
		storePath := a.cfg.Connectors.WhatsApp.StorePath
		children = append(children, whatsapp.New(storePath, a.loader.Supplier()))
		log.Printf("gateway: whatsapp connector (store: %s)", storePath)
	}

	return children, nil
}

func init() {
	gatewayCmd.AddCommand(gatewayStartCmd)
	rootCmd.AddCommand(gatewayCmd)
}
