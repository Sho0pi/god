package socket

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// ErrNoGateway is returned by Dial when nothing is listening on the socket,
// i.e. the gateway is not running.
var ErrNoGateway = errors.New("gateway not running")

// Client is the thin `god cli` side of the control socket. It holds no agent,
// LLM, or config — it just relays text to and from the running gateway.
type Client struct {
	conn net.Conn
	enc  *json.Encoder
	scan *bufio.Scanner
	user string
}

// Dial connects to the gateway socket at path. A connection-refused / missing
// socket is reported as ErrNoGateway so the caller can print a helpful hint.
func Dial(path, user string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isConnRefused(err) {
			return nil, fmt.Errorf("%w (%s)", ErrNoGateway, path)
		}
		return nil, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if user == "" {
		user = "local"
	}
	return &Client{conn: conn, enc: json.NewEncoder(conn), scan: sc, user: user}, nil
}

func isConnRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "no such file")
}

// Send transmits one message frame to the gateway.
func (c *Client) Send(text string) error {
	return c.enc.Encode(clientFrame{User: c.user, Text: text})
}

// Recv blocks for the next reply frame. ok is false at end of stream (gateway
// closed the connection).
func (c *Client) Recv() (text string, ok bool, err error) {
	if !c.scan.Scan() {
		return "", false, c.scan.Err()
	}
	var f serverFrame
	if err := json.Unmarshal(c.scan.Bytes(), &f); err != nil {
		return "", false, err
	}
	return f.Text, true, nil
}

// Close shuts the connection.
func (c *Client) Close() error { return c.conn.Close() }
