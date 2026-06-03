package cli

import (
	"context"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/connector"
)

func waitDone(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnDone never fired — one-shot would hang")
	}
}

// One-shot must signal done even when the handler replies normally.
func TestOneShotWithReply(t *testing.T) {
	done := make(chan struct{})
	c := New(Options{Message: "hi", OnDone: func() { close(done) }})
	c.SetMessageHandler(func(ctx context.Context, msg connector.Message) {
		_ = c.Send(ctx, msg.ChatID, "reply")
	})
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDone(t, done)
	if !c.sent {
		t.Error("sent flag should be true after a reply")
	}
}

// One-shot must still exit when the handler produces no reply (e.g. LLM error
// or a parked approval) — the bug this fix addresses.
func TestOneShotNoReplyStillExits(t *testing.T) {
	done := make(chan struct{})
	c := New(Options{Message: "hi", OnDone: func() { close(done) }})
	c.SetMessageHandler(func(_ context.Context, _ connector.Message) {
		// no Send — simulate an error / parked approval
	})
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDone(t, done)
	if c.sent {
		t.Error("sent flag should be false when nothing was emitted")
	}
}

func TestDefaultUserID(t *testing.T) {
	if got := New(Options{}).opts.UserID; got != "local" {
		t.Errorf("default UserID = %q, want \"local\"", got)
	}
}
