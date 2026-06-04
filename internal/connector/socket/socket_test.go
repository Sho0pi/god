package socket

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/connector"
)

// TestRoundTrip verifies a client message reaches the handler with the right
// identity and that a Send reply travels back to that same connection.
func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "god.sock")
	srv := NewServer(path)

	got := make(chan connector.Message, 1)
	srv.SetMessageHandler(func(_ context.Context, msg connector.Message) {
		got <- msg
		// Echo a reply back to the originating chat.
		if err := srv.Send(context.Background(), msg.ChatID, "pong: "+msg.Text); err != nil {
			t.Errorf("server send: %v", err)
		}
	})

	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop(context.Background()) }()

	client, err := Dial(path, "alice")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Send("ping"); err != nil {
		t.Fatalf("client send: %v", err)
	}

	select {
	case msg := <-got:
		if msg.Connector != "cli" {
			t.Errorf("connector = %q, want cli", msg.Connector)
		}
		if msg.UserID != "alice" {
			t.Errorf("user = %q, want alice", msg.UserID)
		}
		if msg.Text != "ping" {
			t.Errorf("text = %q, want ping", msg.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	reply, ok, err := client.Recv()
	if err != nil || !ok {
		t.Fatalf("recv: ok=%v err=%v", ok, err)
	}
	if reply != "pong: ping" {
		t.Errorf("reply = %q, want %q", reply, "pong: ping")
	}
}

// TestDialNoGateway confirms a missing socket is reported as ErrNoGateway.
func TestDialNoGateway(t *testing.T) {
	_, err := Dial(filepath.Join(t.TempDir(), "absent.sock"), "local")
	if err == nil {
		t.Fatal("expected error dialing absent socket")
	}
}
