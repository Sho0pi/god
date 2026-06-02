package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sho0pi/god/agent"
	"github.com/sho0pi/god/config"
	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/tool"
)

// recordTool counts executions so tests can assert it ran only after approval.
type recordTool struct {
	mu    sync.Mutex
	calls int
}

func (t *recordTool) Name() string         { return "testtool" }
func (t *recordTool) Description() string  { return "test tool" }
func (t *recordTool) Schema() *tool.Schema { return &tool.Schema{} }
func (t *recordTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return "did the thing", nil
}
func (t *recordTool) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// approvalFixture wires an agent where "testtool" requires approval and user
// "u1" is admin. seqResponses drives the LLM.
func approvalFixture(t *testing.T, seq *sequenceLLM) (*mockConnector, *recordTool, func(text string)) {
	t.Helper()
	cfg := &config.Config{
		Admin: []string{"u1"},
		Roles: map[string]config.RoleConfig{"admin": {}}, // empty tools = all
		Souls: map[string]config.SoulConfig{"default": {Prompt: "you are helpful"}},
		Tools: config.ToolsConfig{Approval: []string{"testtool"}},
	}
	conn := newMockConnector()
	rt := &recordTool{}
	reg := tool.NewRegistry()
	reg.Register(rt)

	a := agent.New(conn, seq, reg, nil, nil, agent.Options{
		ConfigFn: func() *config.Config { return cfg },
		MaxTurns: 40,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(text string) {
		conn.handler(context.Background(), connector.Message{
			Connector: "cli", UserID: "u1", ChatID: "c1", SenderID: "u1", Text: text,
		})
	}
	return conn, rt, send
}

func (c *mockConnector) lastText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return ""
	}
	return c.sent[len(c.sent)-1].text
}

func extractApprovalID(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "/approve ")
	if i < 0 {
		t.Fatalf("no approval id in message:\n%s", text)
	}
	fields := strings.Fields(text[i+len("/approve "):])
	if len(fields) == 0 {
		t.Fatalf("empty approval id in:\n%s", text)
	}
	return fields[0]
}

func TestApprovalGate_Approve(t *testing.T) {
	seq := &sequenceLLM{responses: []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "testtool", Args: map[string]any{"x": "hi"}}},
		{Text: "done"},
	}}
	conn, rt, send := approvalFixture(t, seq)

	send("please do it")

	// Parked: tool must NOT have run, an approval prompt must be shown.
	if rt.count() != 0 {
		t.Fatalf("tool ran before approval (calls=%d)", rt.count())
	}
	prompt := conn.lastText()
	if !strings.Contains(prompt, "Approval required") {
		t.Fatalf("expected approval prompt, got:\n%s", prompt)
	}
	id := extractApprovalID(t, prompt)

	// Approve → tool runs, loop resumes to the final answer.
	send("/approve " + id)
	if rt.count() != 1 {
		t.Fatalf("after approve, tool calls=%d, want 1", rt.count())
	}
	if got := conn.lastText(); got != "done" {
		t.Fatalf("after approve, final reply=%q, want \"done\"", got)
	}
}

func TestApprovalGate_Deny(t *testing.T) {
	seq := &sequenceLLM{responses: []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "testtool", Args: map[string]any{"x": "hi"}}},
		{Text: "cancelled then"},
	}}
	conn, rt, send := approvalFixture(t, seq)

	send("please do it")
	id := extractApprovalID(t, conn.lastText())

	send("/deny " + id)
	if rt.count() != 0 {
		t.Fatalf("denied tool must not run, calls=%d", rt.count())
	}
	if got := conn.lastText(); got != "cancelled then" {
		t.Fatalf("after deny, final reply=%q", got)
	}
}

func TestApprovalGate_IDMismatchKeepsPending(t *testing.T) {
	seq := &sequenceLLM{responses: []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "testtool", Args: map[string]any{"x": "hi"}}},
		{Text: "done"},
	}}
	conn, rt, send := approvalFixture(t, seq)

	send("please do it")
	realID := extractApprovalID(t, conn.lastText())

	// Wrong id: nothing runs, pending survives.
	send("/approve deadbeef")
	if rt.count() != 0 {
		t.Fatalf("mismatch must not run tool, calls=%d", rt.count())
	}
	if !strings.Contains(conn.lastText(), "mismatch") {
		t.Fatalf("expected mismatch message, got:\n%s", conn.lastText())
	}

	// Correct id still works afterwards.
	send("/approve " + realID)
	if rt.count() != 1 {
		t.Fatalf("after correct approve, calls=%d, want 1", rt.count())
	}
}

func TestApprovalGate_NormalMessageBlockedWhilePending(t *testing.T) {
	seq := &sequenceLLM{responses: []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "testtool", Args: map[string]any{"x": "hi"}}},
		{Text: "done"},
	}}
	conn, rt, send := approvalFixture(t, seq)

	send("please do it")
	id := extractApprovalID(t, conn.lastText())

	// A normal message while parked must be refused, not processed.
	send("hello there")
	if !strings.Contains(conn.lastText(), "pending approval") {
		t.Fatalf("expected pending-approval refusal, got:\n%s", conn.lastText())
	}
	if rt.count() != 0 {
		t.Fatalf("tool must not run from a blocked message, calls=%d", rt.count())
	}

	// Approval still resolvable.
	send("/approve " + id)
	if rt.count() != 1 {
		t.Fatalf("approve after block failed, calls=%d", rt.count())
	}
}
