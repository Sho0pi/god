package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sho0pi/god/connector"
)

// maxLineBytes caps a single interactive input line. The default bufio.Scanner
// limit is 64KB, which truncates large pastes; 1MB is plenty for chat input.
const maxLineBytes = 1024 * 1024

// Options configures the CLI connector.
type Options struct {
	// UserID is the identity used for messages. Defaults to "local".
	UserID string
	// Message, if set, sends a single message then exits (non-interactive mode).
	Message string
	// OnDone is called when the connector is finished (one-shot reply sent, or
	// interactive input closed). Use it to cancel the outer context so the agent exits.
	OnDone func()
}

type Connector struct {
	handler func(ctx context.Context, msg connector.Message)
	opts    Options
	sent    bool // whether any reply was emitted (one-shot mode)
}

func New(opts Options) *Connector {
	if opts.UserID == "" {
		opts.UserID = "local"
	}
	return &Connector{opts: opts}
}

func (c *Connector) SetMessageHandler(handler func(ctx context.Context, msg connector.Message)) {
	c.handler = handler
}

func (c *Connector) oneShot() bool { return c.opts.Message != "" }

func (c *Connector) message(text string) connector.Message {
	return connector.Message{
		Connector: "cli",
		UserID:    c.opts.UserID,
		ChatID:    "cli",
		SenderID:  c.opts.UserID,
		Text:      text,
	}
}

func (c *Connector) done() {
	if c.opts.OnDone != nil {
		c.opts.OnDone()
	}
}

func (c *Connector) Start(ctx context.Context) error {
	if c.oneShot() {
		// Run the message, then always signal done — even if the handler
		// produced no reply (LLM error) or parked on an approval — so the
		// process never hangs waiting for a Send that won't come.
		go func() {
			if c.handler != nil {
				c.handler(ctx, c.message(c.opts.Message))
			}
			if !c.sent {
				fmt.Println("(no response)")
			}
			c.done()
		}()
		return nil
	}

	fmt.Printf("god — CLI mode (user: %s). Type a message and press Enter. Ctrl+C or Ctrl+D to quit.\n\n", c.opts.UserID)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for {
			fmt.Print("you: ")
			if !scanner.Scan() {
				fmt.Println() // newline after EOF (Ctrl-D) so the prompt isn't dangling
				c.done()      // clean exit instead of hanging on ctx.Done()
				return
			}
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			if c.handler != nil {
				c.handler(ctx, c.message(text))
			}
		}
	}()

	return nil
}

func (c *Connector) Stop(_ context.Context) error {
	return nil
}

func (c *Connector) Send(_ context.Context, _ string, text string) error {
	c.sent = true
	if c.oneShot() {
		fmt.Println(text)
		return nil
	}
	fmt.Printf("\ngod: %s\n\n", text)
	return nil
}
