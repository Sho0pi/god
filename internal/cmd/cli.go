package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/connector/socket"
	"github.com/sho0pi/god/internal/godhome"
)

var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "Talk to the running gateway over its control socket",
	Long: `cli is a thin client: it connects to a "god gateway start" process over
its Unix socket and relays your messages to the shared agent. It holds no LLM,
config, or memory of its own — start the gateway first.`,
	Example: `  god cli                              # interactive chat
  god cli --msg "hello"                # send one message, print reply, exit
  god cli --msg "hi" --user alice      # send as user 'alice'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, _ := cmd.Flags().GetString("msg")
		userID, _ := cmd.Flags().GetString("user")

		path, err := godhome.SocketPath()
		if err != nil {
			return err
		}
		client, err := socket.Dial(path, userID)
		if err != nil {
			if errors.Is(err, socket.ErrNoGateway) {
				return fmt.Errorf("%w — start it with `god gateway start`", err)
			}
			return err
		}
		defer func() { _ = client.Close() }()

		if msg != "" {
			return oneShot(client, msg)
		}
		return interactive(client, userID)
	},
}

// oneShot sends a single message and prints the first reply.
func oneShot(client *socket.Client, msg string) error {
	if err := client.Send(msg); err != nil {
		return err
	}
	text, ok, err := client.Recv()
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("(no response)")
		return nil
	}
	fmt.Println(text)
	return nil
}

// interactive runs a REPL: a goroutine prints incoming replies while the main
// loop reads stdin and sends each line.
func interactive(client *socket.Client, userID string) error {
	fmt.Printf("god — CLI mode (user: %s). Type a message and press Enter. Ctrl+C or Ctrl+D to quit.\n\n", userID)

	go func() {
		for {
			text, ok, err := client.Recv()
			if err != nil || !ok {
				return
			}
			fmt.Printf("\ngod: %s\n\nyou: ", text)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("you: ")
		if !scanner.Scan() {
			fmt.Println()
			return scanner.Err()
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if err := client.Send(text); err != nil {
			return err
		}
	}
}

func init() {
	cliCmd.Flags().StringP("msg", "m", "", "send a single message and exit (non-interactive)")
	cliCmd.Flags().StringP("user", "u", "local", "userID to send as (creates user if not exists)")
	rootCmd.AddCommand(cliCmd)
}
