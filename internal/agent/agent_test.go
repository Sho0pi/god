package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/agent"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/llm"
	tools "github.com/sho0pi/god/internal/tools"
)

// --- mock LLM ---

type mockLLM struct {
	mu          sync.Mutex
	response    *llm.Response
	calls       []llm.Message
	lastHistory []llm.Message
	systemCalls []string
}

func (m *mockLLM) Chat(ctx context.Context, history []llm.Message, tools []tools.Tool) (*llm.Response, error) {
	return m.ChatWithSystem(ctx, "", history, tools)
}

func (m *mockLLM) ChatWithSystem(_ context.Context, system string, history []llm.Message, _ []tools.Tool) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, history...)
	m.lastHistory = make([]llm.Message, len(history))
	copy(m.lastHistory, history)
	m.systemCalls = append(m.systemCalls, system)
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

// --- mock store ---

type mockStore struct {
	mu    sync.Mutex
	saved []savedMemory
}

type savedMemory struct {
	connector string
	userID    string
	fact      string
}

func (s *mockStore) AssignSoul(_ context.Context, _, _, _ string) error     { return nil }
func (s *mockStore) GetSoul(_ context.Context, _, _ string) (string, error) { return "", nil }
func (s *mockStore) DeleteSoul(_ context.Context, _, _ string) error        { return nil }
func (s *mockStore) AssignRole(_ context.Context, _, _, _ string) error     { return nil }
func (s *mockStore) GetRole(_ context.Context, _, _ string) (string, error) { return "", nil }
func (s *mockStore) DeleteRole(_ context.Context, _, _ string) error        { return nil }
func (s *mockStore) DeleteMemories(_ context.Context, _, _ string) error    { return nil }
func (s *mockStore) SaveMemory(_ context.Context, conn, userID, fact string, _ []float32) error {
	s.mu.Lock()
	s.saved = append(s.saved, savedMemory{conn, userID, fact})
	s.mu.Unlock()
	return nil
}
func (s *mockStore) SearchMemories(_ context.Context, _, _ string, _ []float32, _ int) ([]string, error) {
	return nil, nil
}
func (s *mockStore) AddAllow(_ context.Context, _, _ string) error           { return nil }
func (s *mockStore) RemoveAllow(_ context.Context, _, _ string) error        { return nil }
func (s *mockStore) ListAllow(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (s *mockStore) ResolveIdentity(_ context.Context, c, u string) (string, string, error) {
	return c, u, nil
}
func (s *mockStore) Link(_ context.Context, _, _, _, _ string) error { return nil }
func (s *mockStore) Unlink(_ context.Context, _, _ string) error     { return nil }
func (s *mockStore) Close() error                                    { return nil }

// --- mock embedder ---

type mockEmbedder struct{}

func (e *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

// --- tests ---

func TestAgent_RespondsToMessage(t *testing.T) {
	lm := &mockLLM{response: &llm.Response{Text: "hello back"}}
	conn := newMockConnector()
	registry := tools.NewRegistry()

	a := agent.New(conn, lm, registry, nil, nil, agent.Options{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	conn.waitReady(t)

	conn.handler(ctx, connector.Message{
		Connector: "test",
		UserID:    "user1",
		ChatID:    "chat1",
		SenderID:  "user1",
		Text:      "hi",
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
	registry := tools.NewRegistry()

	a := agent.New(conn, lm, registry, nil, nil, agent.Options{})
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

	send("user1", "message 1")
	time.Sleep(50 * time.Millisecond)
	send("user1", "message 2")
	time.Sleep(50 * time.Millisecond)
	send("user2", "hello")
	time.Sleep(100 * time.Millisecond)

	cancel()

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.sent) < 2 {
		t.Errorf("expected at least 2 replies, got %d", len(conn.sent))
	}
}

func TestAgent_ToolCallLoop(t *testing.T) {
	conn := newMockConnector()
	registry := tools.NewRegistry()

	responses := []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "fake_tool", Args: map[string]any{}}},
		{Text: "done"},
	}

	seqLLM := &sequenceLLM{responses: responses}
	a := agent.New(conn, seqLLM, registry, nil, nil, agent.Options{})

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
	if conn.sent[0].text != "done" {
		t.Errorf("reply = %q, want 'done'", conn.sent[0].text)
	}
}

func TestAgent_SlidingWindow(t *testing.T) {
	// Track each ChatWithSystem call's history length separately.
	type callRecord struct {
		histLen int
		system  string
	}
	var mu sync.Mutex
	var callRecords []callRecord

	captureLLM := &capturingLLM{
		fn: func(system string, history []llm.Message) (*llm.Response, error) {
			mu.Lock()
			callRecords = append(callRecords, callRecord{len(history), system})
			mu.Unlock()
			return &llm.Response{Text: "ok"}, nil
		},
	}

	conn := newMockConnector()
	// maxTurns=2 → keeps last 4 messages (2 user + 2 model)
	a := agent.New(conn, captureLLM, tools.NewRegistry(), nil, nil, agent.Options{MaxTurns: 2})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(text string) {
		conn.handler(ctx, connector.Message{
			Connector: "test", UserID: "u1", ChatID: "c1", SenderID: "u1", Text: text,
		})
		time.Sleep(30 * time.Millisecond)
	}

	// Send 5 messages. After the 3rd, window should cap at maxTurns*2=4 messages.
	for i := range 5 {
		_ = i
		send("msg")
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(callRecords) < 5 {
		t.Fatalf("expected 5 LLM calls, got %d", len(callRecords))
	}
	// First call: 1 msg (just the user message, no history yet)
	// After window kicks in (turn 3+): history passed to LLM must be ≤ maxTurns*2+1
	// (+1 because current user msg is appended before trim)
	maxAllowed := 2*2 + 1 // maxTurns*2 stored + current user msg
	last := callRecords[len(callRecords)-1]
	if last.histLen > maxAllowed {
		t.Errorf("last call history len = %d, want ≤ %d (sliding window not working)", last.histLen, maxAllowed)
	}
}

func TestAgent_InactivityTimer(t *testing.T) {
	// First call: normal chat response. Second call: extraction pass.
	seqLLM := &sequenceLLM{responses: []*llm.Response{
		{Text: "hi there"},
		{Text: "User likes testing\nUser works on god project"},
	}}

	store := &mockStore{}
	embedder := &mockEmbedder{}
	conn := newMockConnector()

	a := agent.New(conn, seqLLM, tools.NewRegistry(), embedder, store, agent.Options{
		InactivityTimeout: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	conn.handler(ctx, connector.Message{
		Connector: "test", UserID: "u1", ChatID: "c1", SenderID: "u1",
		Text: "hello",
	})

	// Wait for reply + timer to fire + extraction to complete.
	time.Sleep(300 * time.Millisecond)
	cancel()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) == 0 {
		t.Error("expected facts to be extracted and saved after inactivity timeout")
	}
	// Verify facts are attributed to the right user.
	for _, m := range store.saved {
		if m.connector != "test" || m.userID != "u1" {
			t.Errorf("memory saved with wrong identity: connector=%q userID=%q", m.connector, m.userID)
		}
	}
}

func TestAgent_InactivityTimerResets(t *testing.T) {
	// Verify timer resets on each message — extraction only fires after final silence.
	var extractionCount int
	var mu sync.Mutex

	seqLLM := &capturingLLM{
		fn: func(system string, history []llm.Message) (*llm.Response, error) {
			if strings.HasPrefix(system, "Extract important facts") {
				mu.Lock()
				extractionCount++
				mu.Unlock()
				return &llm.Response{Text: "User prefers Go"}, nil
			}
			return &llm.Response{Text: "ok"}, nil
		},
	}

	store := &mockStore{}
	embedder := &mockEmbedder{}
	conn := newMockConnector()

	timeout := 80 * time.Millisecond
	a := agent.New(conn, seqLLM, tools.NewRegistry(), embedder, store, agent.Options{
		InactivityTimeout: timeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(text string) {
		conn.handler(ctx, connector.Message{
			Connector: "test", UserID: "u1", ChatID: "c1", SenderID: "u1", Text: text,
		})
	}

	// Send 3 messages quickly (< timeout apart) then go silent.
	send("msg1")
	time.Sleep(30 * time.Millisecond)
	send("msg2")
	time.Sleep(30 * time.Millisecond)
	send("msg3")

	// Wait for final timer to fire.
	time.Sleep(300 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	// Only one extraction should have fired (the final one after silence).
	if extractionCount != 1 {
		t.Errorf("extraction fired %d times, want 1", extractionCount)
	}
}

func TestAgent_ResetCommand(t *testing.T) {
	lm := &mockLLM{response: &llm.Response{Text: "ok"}}
	conn := newMockConnector()

	a := agent.New(conn, lm, tools.NewRegistry(), nil, nil, agent.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(text string) {
		conn.handler(ctx, connector.Message{
			Connector: "test", UserID: "u1", ChatID: "c1", SenderID: "u1", Text: text,
		})
		time.Sleep(30 * time.Millisecond)
	}

	// Build up some history.
	send("hello")
	send("how are you")

	// Reset should clear history — next LLM call should get only 1 message (no prior context).
	send("/reset")

	// Capture history length on next real message.
	lm.mu.Lock()
	lm.lastHistory = nil
	lm.mu.Unlock()

	send("fresh start")
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Verify reset reply was sent.
	conn.mu.Lock()
	defer conn.mu.Unlock()
	var resetReply bool
	for _, m := range conn.sent {
		if strings.Contains(strings.ToLower(m.text), "clear") || strings.Contains(strings.ToLower(m.text), "reset") {
			resetReply = true
		}
	}
	if !resetReply {
		t.Error("expected reset confirmation message")
	}

	// Verify history was cleared: post-reset message should have only 1 entry.
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if len(lm.lastHistory) != 1 {
		t.Errorf("after reset, history len = %d, want 1 (only fresh message)", len(lm.lastHistory))
	}
}

// --- helpers ---

// capturingLLM calls fn for each ChatWithSystem invocation.
type capturingLLM struct {
	fn func(system string, history []llm.Message) (*llm.Response, error)
}

func (c *capturingLLM) Chat(ctx context.Context, history []llm.Message, tools []tools.Tool) (*llm.Response, error) {
	return c.ChatWithSystem(ctx, "", history, tools)
}

func (c *capturingLLM) ChatWithSystem(_ context.Context, system string, history []llm.Message, _ []tools.Tool) (*llm.Response, error) {
	return c.fn(system, history)
}

// sequenceLLM returns responses in order.
type sequenceLLM struct {
	mu        sync.Mutex
	responses []*llm.Response
	idx       int
}

func (s *sequenceLLM) Chat(ctx context.Context, history []llm.Message, tools []tools.Tool) (*llm.Response, error) {
	return s.ChatWithSystem(ctx, "", history, tools)
}

func (s *sequenceLLM) ChatWithSystem(_ context.Context, _ string, _ []llm.Message, _ []tools.Tool) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.responses) {
		return &llm.Response{Text: "fallback"}, nil
	}
	r := s.responses[s.idx]
	s.idx++
	return r, nil
}
