package multi

import (
	"context"
	"testing"

	"github.com/sho0pi/god/internal/connector"
)

// fakeConn records sends and lets the test inject inbound messages.
type fakeConn struct {
	name    string
	handler func(context.Context, connector.Message)
	sent    []string
	allow   func() []string
}

func (f *fakeConn) Start(context.Context) error { return nil }
func (f *fakeConn) Stop(context.Context) error  { return nil }
func (f *fakeConn) Send(_ context.Context, _, text string) error {
	f.sent = append(f.sent, f.name+":"+text)
	return nil
}
func (f *fakeConn) SetMessageHandler(h func(context.Context, connector.Message)) { f.handler = h }
func (f *fakeConn) SetAllowSource(fn func() []string)                            { f.allow = fn }

// TestSendRoutesToOriginatingChild verifies a reply goes only to the child that
// produced the message for that chat.
func TestSendRoutesToOriginatingChild(t *testing.T) {
	a := &fakeConn{name: "a"}
	b := &fakeConn{name: "b"}
	m := New(a, b)

	var seen int
	m.SetMessageHandler(func(_ context.Context, _ connector.Message) { seen++ })

	// Inbound from child b for chat "x".
	b.handler(context.Background(), connector.Message{Connector: "b", ChatID: "x"})
	if seen != 1 {
		t.Fatalf("handler calls = %d, want 1", seen)
	}

	if err := m.Send(context.Background(), "x", "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(a.sent) != 0 {
		t.Errorf("child a got %v, want none", a.sent)
	}
	if len(b.sent) != 1 || b.sent[0] != "b:hi" {
		t.Errorf("child b sent = %v, want [b:hi]", b.sent)
	}
}

// TestSendUnknownChat errors rather than guessing a child.
func TestSendUnknownChat(t *testing.T) {
	m := New(&fakeConn{name: "a"})
	if err := m.Send(context.Background(), "ghost", "x"); err == nil {
		t.Fatal("expected error for unrouted chat")
	}
}

// TestSetAllowSourceFansOut forwards the source to every supporting child.
func TestSetAllowSourceFansOut(t *testing.T) {
	a := &fakeConn{name: "a"}
	m := New(a)
	m.SetAllowSource(func() []string { return []string{"x"} })
	if a.allow == nil {
		t.Fatal("allow source not forwarded")
	}
}
