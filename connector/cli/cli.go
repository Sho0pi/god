package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sho0pi/god/connector"
)

type Connector struct {
	handler func(ctx context.Context, msg connector.Message)
}

func New() *Connector {
	return &Connector{}
}

func (c *Connector) SetMessageHandler(handler func(ctx context.Context, msg connector.Message)) {
	c.handler = handler
}

func (c *Connector) Start(ctx context.Context) error {
	fmt.Println("god — CLI mode. Type a message and press Enter. Ctrl+C to quit.")
	fmt.Println()

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("you: ")
			if !scanner.Scan() {
				return
			}
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			if c.handler != nil {
				c.handler(ctx, connector.Message{
					ChatID:   "cli",
					SenderID: "user",
					Text:     text,
				})
			}
		}
	}()

	return nil
}

func (c *Connector) Stop(_ context.Context) error {
	return nil
}

func (c *Connector) Send(_ context.Context, _ string, text string) error {
	fmt.Printf("\ngod: %s\n\n", text)
	return nil
}
