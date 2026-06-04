// Package multi fans several connectors into one. The agent owns a single
// connector.Connector; Multi lets the gateway run WhatsApp + the control socket
// (and future connectors) through that one seam. Inbound messages from every
// child share one handler (each Message already carries its Connector name);
// outbound Send is routed back to the child that owns the destination chat.
package multi

import (
	"context"
	"fmt"
	"sync"

	"github.com/sho0pi/god/internal/connector"
)

// Multi implements connector.Connector by multiplexing child connectors.
type Multi struct {
	children []connector.Connector
	handler  func(ctx context.Context, msg connector.Message)

	mu    sync.RWMutex
	route map[string]connector.Connector // chatID → owning child
}

// New builds a Multi over the given children.
func New(children ...connector.Connector) *Multi {
	return &Multi{children: children, route: make(map[string]connector.Connector)}
}

// SetMessageHandler installs one handler shared by all children. Each child is
// given a wrapper that first records which child owns the message's chat (so a
// later Send can be routed back to it) and then invokes the shared handler.
func (m *Multi) SetMessageHandler(handler func(ctx context.Context, msg connector.Message)) {
	m.handler = handler
	for _, child := range m.children {
		child.SetMessageHandler(func(ctx context.Context, msg connector.Message) {
			m.mu.Lock()
			m.route[msg.ChatID] = child
			m.mu.Unlock()
			handler(ctx, msg)
		})
	}
}

// Start starts every child. If any fails, already-started children are stopped
// so the gateway never half-runs.
func (m *Multi) Start(ctx context.Context) error {
	started := make([]connector.Connector, 0, len(m.children))
	for _, child := range m.children {
		if err := child.Start(ctx); err != nil {
			for _, s := range started {
				_ = s.Stop(context.Background())
			}
			return fmt.Errorf("multi: start child: %w", err)
		}
		started = append(started, child)
	}
	return nil
}

// Stop stops every child, returning the first error (after attempting all).
func (m *Multi) Stop(ctx context.Context) error {
	var firstErr error
	for _, child := range m.children {
		if err := child.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Send routes the reply to the child that produced the message for chatID.
func (m *Multi) Send(ctx context.Context, chatID, text string) error {
	m.mu.RLock()
	child := m.route[chatID]
	m.mu.RUnlock()
	if child == nil {
		return fmt.Errorf("multi: no route for chat %q", chatID)
	}
	return child.Send(ctx, chatID, text)
}

// SetAllowSource forwards a runtime allow-list source to any child that
// supports one (currently WhatsApp), so the /allow admin command keeps working
// when connectors run behind the gateway.
func (m *Multi) SetAllowSource(fn func() []string) {
	for _, child := range m.children {
		if as, ok := child.(interface{ SetAllowSource(func() []string) }); ok {
			as.SetAllowSource(fn)
		}
	}
}
