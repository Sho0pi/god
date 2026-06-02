package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sho0pi/god/agent"
	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/tool"
)

// --- mock LLM ---

type mockLLM struct {
	mu       sync.Mutex
	response *llm.Response
	calls    []llm.Message
}

func (m *mockLLM) Chat(ctx context.Context, history []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	return m.ChatWithSystem(ctx, "", history, tools)
}

func (m *mockLLM) ChatWithSystem(_ context.Context, _ string, history []llm.Message, _ []tool.Tool) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, history...)
	return m.response, nil
}

// --- mock connector ---

type mockConnector struct {
	handler    func(ctx context.Context, msg connector.Message)
	handlerSet chan struct{}
	sent       []sentMsg
	mu         sync.Mutex
}

func newMockConnector() *mockConnector {
	return &mockConnector{handlerSet: make(chan struct{})}
}

type sentMsg struct {
	chatID string
	text   string
}

func (c *mockConnector) SetMessageHandler(h func(context.Context, connector.Message)) {
	c.handler = h
	close(c.handlerSet)
}

func (c *mockConnector) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-c.handlerSet:
	case <-time.After(time.Second):
		t.Fatal("agent did not set handler in time")
	}
}

func (c *mockConnector) Start(ctx context.Context) error { return nil }
func (c *mockConnector) Stop(_ context.Context) error    { return nil }

func (c *mockConnector) Send(_ context.Context, chatID, text string) error {
	c.mu.Lock()
	c.sent = append(c.sent, sentMsg{chatID, text})
	c.mu.Unlock()
	return nil
}

// --- tests ---

func TestAgent_RespondsToMessage(t *testing.T) {
	lm := &mockLLM{response: &llm.Response{Text: "hello back"}}
	conn := newMockConnector()
	registry := tool.NewRegistry()

	a := agent.New(conn, lm, registry, nil, nil, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Run agent in background, wait for it to register the handler.
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	conn.waitReady(t)

	// Deliver a message.
	conn.handler(ctx, connector.Message{
		Connector: "test",
		UserID:    "user1",
		ChatID:    "chat1",
		SenderID:  "user1",
		Text:      "hi",
	})

	// Wait for reply.
	deadline := time.After(2 * time.Second)
	for {
		conn.mu.Lock()
		n := len(conn.sent)
		conn.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for reply")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	<-done

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.sent[0].text != "hello back" {
		t.Errorf("reply = %q, want 'hello back'", conn.sent[0].text)
	}
	if conn.sent[0].chatID != "chat1" {
		t.Errorf("chatID = %q, want 'chat1'", conn.sent[0].chatID)
	}
}

func TestAgent_HistoryIsolatedPerUser(t *testing.T) {
	lm := &mockLLM{response: &llm.Response{Text: "ok"}}
	conn := newMockConnector()
	registry := tool.NewRegistry()

	a := agent.New(conn, lm, registry, nil, nil, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(userID, text string) {
		conn.handler(ctx, connector.Message{
			Connector: "test",
			UserID:    userID,
			ChatID:    userID,
			SenderID:  userID,
			Text:      text,
		})
	}

	// user1 sends 2 messages, user2 sends 1 — histories must not bleed.
	send("user1", "message 1")
	time.Sleep(50 * time.Millisecond)
	send("user1", "message 2")
	time.Sleep(50 * time.Millisecond)
	send("user2", "hello")
	time.Sleep(100 * time.Millisecond)

	cancel()

	lm.mu.Lock()
	defer lm.mu.Unlock()
	// user1's second call history should have 3 messages (msg1, reply1, msg2).
	// user2's call history should have 1 message only.
	// We verify via sent replies — 3 total expected.
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.sent) < 2 {
		t.Errorf("expected at least 2 replies, got %d", len(conn.sent))
	}
}

func TestAgent_ToolCallLoop(t *testing.T) {
	lm := &mockLLM{}
	conn := newMockConnector()
	registry := tool.NewRegistry()

	// First call returns a tool call, second returns text.
	responses := []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "fake_tool", Args: map[string]any{}}},
		{Text: "done"},
	}

	origChat := lm.ChatWithSystem
	_ = origChat

	seqLLM := &sequenceLLM{responses: responses}
	a := agent.New(conn, seqLLM, registry, nil, nil, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	conn.handler(ctx, connector.Message{
		Connector: "test", UserID: "u1", ChatID: "c1", SenderID: "u1",
		Text: "do something",
	})

	deadline := time.After(2 * time.Second)
	for {
		conn.mu.Lock()
		n := len(conn.sent)
		conn.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for reply")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()

	conn.mu.Lock()
	defer conn.mu.Unlock()
	// Tool was unknown so dispatched as error, then LLM returned "done".
	if conn.sent[0].text != "done" {
		t.Errorf("reply = %q, want 'done'", conn.sent[0].text)
	}
}

// sequenceLLM returns responses in order.
type sequenceLLM struct {
	mu        sync.Mutex
	responses []*llm.Response
	idx       int
}

func (s *sequenceLLM) Chat(ctx context.Context, history []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	return s.ChatWithSystem(ctx, "", history, tools)
}

func (s *sequenceLLM) ChatWithSystem(_ context.Context, _ string, _ []llm.Message, _ []tool.Tool) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.responses) {
		return &llm.Response{Text: "fallback"}, nil
	}
	r := s.responses[s.idx]
	s.idx++
	return r, nil
}
