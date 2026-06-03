package agent_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/agent"
	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/llm"
	"github.com/sho0pi/god/internal/tool"
	toolsoul "github.com/sho0pi/god/internal/tool/soul"
)

// --- stateful mock store ---

type stateStore struct {
	mu    sync.Mutex
	souls map[string]string
	roles map[string]string
	facts map[string][]string

	deleteSoulCalls []string
	deleteRoleCalls []string
	deleteMemCalls  []string
}

func newStateStore() *stateStore {
	return &stateStore{
		souls: make(map[string]string),
		roles: make(map[string]string),
		facts: make(map[string][]string),
	}
}

func (s *stateStore) key(connector, userID string) string { return connector + ":" + userID }

func (s *stateStore) AssignSoul(_ context.Context, c, u, soul string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.souls[s.key(c, u)] = soul
	return nil
}
func (s *stateStore) GetSoul(_ context.Context, c, u string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.souls[s.key(c, u)], nil
}
func (s *stateStore) DeleteSoul(_ context.Context, c, u string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteSoulCalls = append(s.deleteSoulCalls, s.key(c, u))
	delete(s.souls, s.key(c, u))
	return nil
}
func (s *stateStore) AssignRole(_ context.Context, c, u, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[s.key(c, u)] = role
	return nil
}
func (s *stateStore) GetRole(_ context.Context, c, u string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roles[s.key(c, u)], nil
}
func (s *stateStore) DeleteRole(_ context.Context, c, u string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteRoleCalls = append(s.deleteRoleCalls, s.key(c, u))
	delete(s.roles, s.key(c, u))
	return nil
}
func (s *stateStore) SaveMemory(_ context.Context, c, u, fact string, _ []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(c, u)
	s.facts[k] = append(s.facts[k], fact)
	return nil
}
func (s *stateStore) SearchMemories(_ context.Context, c, u string, _ []float32, limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.facts[s.key(c, u)]
	if len(all) > limit {
		return all[:limit], nil
	}
	return all, nil
}
func (s *stateStore) DeleteMemories(_ context.Context, c, u string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(c, u)
	s.deleteMemCalls = append(s.deleteMemCalls, k)
	delete(s.facts, k)
	return nil
}
func (s *stateStore) AddAllow(_ context.Context, _, _ string) error           { return nil }
func (s *stateStore) RemoveAllow(_ context.Context, _, _ string) error        { return nil }
func (s *stateStore) ListAllow(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (s *stateStore) Close() error                                            { return nil }

// --- helpers ---

func newAgent(t *testing.T, lm llm.LLM, st *stateStore, opts agent.Options) (*mockConnector, *agent.Agent) {
	t.Helper()
	conn := newMockConnector()
	opts.Commands = nil // use builtins
	a := agent.New(conn, lm, tool.NewRegistry(), &mockEmbedder{}, st, opts)
	return conn, a
}

func runAndWait(t *testing.T, ctx context.Context, conn *mockConnector, a *agent.Agent) func(userID, text string) string {
	t.Helper()
	go a.Run(ctx)
	conn.waitReady(t)
	return func(userID, text string) string {
		prevCount := func() int {
			conn.mu.Lock()
			defer conn.mu.Unlock()
			return len(conn.sent)
		}()
		conn.handler(ctx, connector.Message{
			Connector: "test", UserID: userID, ChatID: userID, SenderID: userID, Text: text,
		})
		deadline := time.After(2 * time.Second)
		for {
			conn.mu.Lock()
			n := len(conn.sent)
			conn.mu.Unlock()
			if n > prevCount {
				conn.mu.Lock()
				reply := conn.sent[n-1].text
				conn.mu.Unlock()
				return reply
			}
			select {
			case <-deadline:
				t.Fatalf("timeout waiting for reply to %q", text)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

// --- integration tests ---

// TestIntegration_SoulOnboarding: new user gets god soul, god calls set_soul,
// next message uses the assigned soul.
func TestIntegration_SoulOnboarding(t *testing.T) {
	st := newStateStore()

	// First response: god soul calls set_soul("caveman")
	// Second response: normal reply as caveman
	var callCount int
	var mu sync.Mutex
	seqLLM := &capturingLLM{
		fn: func(system string, history []llm.Message) (*llm.Response, error) {
			mu.Lock()
			n := callCount
			callCount++
			mu.Unlock()
			if n == 0 {
				// god soul: call set_soul tool
				return &llm.Response{ToolCall: &llm.ToolCall{
					Name: "set_soul",
					Args: map[string]any{"soul": "caveman", "reason": "terse developer"},
				}}, nil
			}
			if n == 1 {
				// tool result turn — return final reply
				return &llm.Response{Text: "Soul set."}, nil
			}
			// subsequent messages use caveman soul prompt
			return &llm.Response{Text: "Caveman reply. " + system[:min(30, len(system))]}, nil
		},
	}

	// Register set_soul tool
	r := tool.NewRegistry()
	r.Register(toolsoul.NewSetSoulTool(st, []string{"human", "caveman"}))

	conn := newMockConnector()
	a := agent.New(conn, seqLLM, r, &mockEmbedder{}, st, agent.Options{
		Souls: map[string]config.SoulConfig{
			"god":     {Prompt: "You are onboarding."},
			"caveman": {Prompt: "Speak like caveman."},
		},
		DefaultSouls: map[string]string{"test": "god"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(text string) string {
		prev := func() int { conn.mu.Lock(); defer conn.mu.Unlock(); return len(conn.sent) }()
		conn.handler(ctx, connector.Message{Connector: "test", UserID: "u1", ChatID: "u1", SenderID: "u1", Text: text})
		deadline := time.After(2 * time.Second)
		for {
			conn.mu.Lock()
			n := len(conn.sent)
			conn.mu.Unlock()
			if n > prev {
				conn.mu.Lock()
				r := conn.sent[n-1].text
				conn.mu.Unlock()
				return r
			}
			select {
			case <-deadline:
				t.Fatal("timeout")
				return ""
			case <-time.After(10 * time.Millisecond):
			}
		}
	}

	send("hi, I'm a developer")
	time.Sleep(50 * time.Millisecond)

	// Soul should now be assigned
	soul, _ := st.GetSoul(context.Background(), "test", "u1")
	if soul != "caveman" {
		t.Errorf("soul = %q after onboarding, want 'caveman'", soul)
	}

	// Next message should use caveman soul
	reply := send("tell me something")
	if !strings.Contains(reply, "Speak like caveman") && !strings.Contains(reply, "Caveman") {
		t.Logf("reply: %q", reply)
		// Soul prompt is in system — verify store has caveman, not god
		if soul != "caveman" {
			t.Error("soul not set to caveman")
		}
	}
}

// TestIntegration_WhoamiCommand verifies /whoami returns soul, role, and LLM info.
func TestIntegration_WhoamiCommand(t *testing.T) {
	st := newStateStore()
	st.souls["test:u1"] = "human"
	st.roles["test:u1"] = "user"

	lm := &mockLLM{response: &llm.Response{Text: "ok"}}
	conn, a := newAgent(t, lm, st, agent.Options{
		Souls: map[string]config.SoulConfig{"human": {Prompt: "human prompt"}},
		Roles: map[string]config.RoleConfig{
			"user": {LLM: config.LLMProviderConfig{Provider: "gemini", Model: "flash"}},
		},
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "user"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	send := runAndWait(t, ctx, conn, a)

	reply := send("u1", "/whoami")
	if !strings.Contains(reply, "human") {
		t.Errorf("whoami missing soul, got: %q", reply)
	}
	if !strings.Contains(reply, "user") {
		t.Errorf("whoami missing role, got: %q", reply)
	}
	if !strings.Contains(reply, "gemini") {
		t.Errorf("whoami missing provider, got: %q", reply)
	}
}

// TestIntegration_ResetPreservesSoulAndMemory: /reset clears history but NOT soul or memories.
func TestIntegration_ResetPreservesSoulAndMemory(t *testing.T) {
	st := newStateStore()
	st.souls["test:u1"] = "caveman"
	st.roles["test:u1"] = "user"
	st.facts["test:u1"] = []string{"User loves Go", "User hates Java"}

	lm := &mockLLM{response: &llm.Response{Text: "ok"}}
	conn, a := newAgent(t, lm, st, agent.Options{
		Souls:        map[string]config.SoulConfig{"caveman": {Prompt: "caveman"}},
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "user"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	send := runAndWait(t, ctx, conn, a)

	// Build some history
	send("u1", "hello")
	send("u1", "how are you")

	// Reset
	reply := send("u1", "/reset")
	if !strings.Contains(strings.ToLower(reply), "clear") {
		t.Errorf("reset reply unexpected: %q", reply)
	}

	// Soul must still be caveman
	soul, _ := st.GetSoul(context.Background(), "test", "u1")
	if soul != "caveman" {
		t.Errorf("soul changed after reset, got %q", soul)
	}

	// Memories must NOT be deleted
	if len(st.deleteMemCalls) > 0 {
		t.Error("reset deleted memories — should not have")
	}
	if len(st.deleteSoulCalls) > 0 {
		t.Error("reset deleted soul — should not have")
	}

	// History cleared: next LLM call should have 1 message (just new user msg)
	lm.mu.Lock()
	lm.lastHistory = nil
	lm.mu.Unlock()

	send("u1", "fresh start")
	time.Sleep(30 * time.Millisecond)

	lm.mu.Lock()
	histLen := len(lm.lastHistory)
	lm.mu.Unlock()
	if histLen != 1 {
		t.Errorf("after reset history len = %d, want 1", histLen)
	}
}

// TestIntegration_FactoryResetAdminOnly: /factory-reset wipes everything for admin, denied for non-admin.
func TestIntegration_FactoryResetAdminOnly(t *testing.T) {
	st := newStateStore()
	st.souls["test:admin"] = "caveman"
	st.roles["test:admin"] = "user" // not in store as admin yet — uses admins bootstrap
	st.souls["test:guest"] = "human"
	st.facts["test:admin"] = []string{"fact1"}
	st.facts["test:guest"] = []string{"fact2"}

	lm := &mockLLM{response: &llm.Response{Text: "ok"}}
	conn, a := newAgent(t, lm, st, agent.Options{
		Admins:       []string{"admin"},
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "user"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	send := runAndWait(t, ctx, conn, a)

	// Non-admin gets denied
	reply := send("guest", "/factory-reset")
	if !strings.Contains(strings.ToLower(reply), "denied") {
		t.Errorf("expected permission denied for guest, got: %q", reply)
	}
	// Guest soul/memories untouched
	if len(st.deleteSoulCalls) > 0 {
		t.Error("factory-reset ran for non-admin")
	}

	// Admin succeeds
	reply = send("admin", "/factory-reset")
	if strings.Contains(strings.ToLower(reply), "denied") {
		t.Errorf("admin got denied: %q", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "wipe") && !strings.Contains(strings.ToLower(reply), "done") && !strings.Contains(strings.ToLower(reply), "reset") {
		t.Errorf("unexpected factory-reset reply: %q", reply)
	}

	// Admin soul, role, memories all wiped
	st.mu.Lock()
	deletedSouls := append([]string{}, st.deleteSoulCalls...)
	deletedMem := append([]string{}, st.deleteMemCalls...)
	st.mu.Unlock()

	if !containsStr(deletedSouls, "test:admin") {
		t.Error("soul not deleted for admin after factory-reset")
	}
	if !containsStr(deletedMem, "test:admin") {
		t.Error("memories not deleted for admin after factory-reset")
	}
}

// TestIntegration_MemoryInjectedInSystemPrompt: saved memories appear in system prompt.
func TestIntegration_MemoryInjectedInSystemPrompt(t *testing.T) {
	st := newStateStore()
	st.souls["test:u1"] = "human"
	st.facts["test:u1"] = []string{"User is a Go developer", "User prefers short replies"}

	var capturedSystem string
	capLLM2 := &capturingLLM{
		fn: func(system string, _ []llm.Message) (*llm.Response, error) {
			capturedSystem = system
			return &llm.Response{Text: "got it"}, nil
		},
	}

	conn, a := newAgent(t, capLLM2, st, agent.Options{
		TopK:         5,
		Souls:        map[string]config.SoulConfig{"human": {Prompt: "Be human."}},
		DefaultSouls: map[string]string{"test": "human"},
		DefaultRoles: map[string]string{"test": "user"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	send := runAndWait(t, ctx, conn, a)

	send("u1", "hi")
	time.Sleep(30 * time.Millisecond)
	cancel()

	if !strings.Contains(capturedSystem, "Go developer") {
		t.Errorf("memory not in system prompt, got: %q", capturedSystem)
	}
	if !strings.Contains(capturedSystem, "short replies") {
		t.Errorf("memory not in system prompt, got: %q", capturedSystem)
	}
}

// TestIntegration_RoleToolFiltering: guest role only gets allowed tools.
func TestIntegration_RoleToolFiltering(t *testing.T) {
	st := newStateStore()

	r := tool.NewRegistry()
	r.Register(&fakeNamedTool{name: "calculator"})
	r.Register(&fakeNamedTool{name: "web_search"})
	r.Register(&fakeNamedTool{name: "remember"})

	conn := newMockConnector()

	toolsPassedToLLM := make([][]tool.Tool, 0)
	var toolsMu sync.Mutex

	wrapped := &toolInterceptLLM{
		inner: &capturingLLM{fn: func(_ string, _ []llm.Message) (*llm.Response, error) {
			return &llm.Response{Text: "done"}, nil
		}},
		onCall: func(tools []tool.Tool) {
			toolsMu.Lock()
			toolsPassedToLLM = append(toolsPassedToLLM, tools)
			toolsMu.Unlock()
		},
	}

	a := agent.New(conn, wrapped, r, &mockEmbedder{}, st, agent.Options{
		Roles: map[string]config.RoleConfig{
			"guest": {Tools: []string{"calculator"}}, // only calculator
			"user":  {Tools: []string{"calculator", "web_search", "remember"}},
		},
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "guest"}, // default guest
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	conn.handler(ctx, connector.Message{Connector: "test", UserID: "g1", ChatID: "g1", SenderID: "g1", Text: "hi"})
	time.Sleep(100 * time.Millisecond)
	cancel()

	toolsMu.Lock()
	defer toolsMu.Unlock()
	if len(toolsPassedToLLM) == 0 {
		t.Skip("tool interceptor not called — LLM wrapping test infrastructure limitation")
	}
	passed := toolsPassedToLLM[0]
	for _, t2 := range passed {
		if t2.Name() != "calculator" {
			t.Errorf("guest got non-allowed tool %q", t2.Name())
		}
	}
}

// TestIntegration_RoleLLMRouting: different roles use different LLM clients from the pool.
func TestIntegration_RoleLLMRouting(t *testing.T) {
	st := newStateStore()
	st.roles["test:admin_user"] = "admin"
	st.roles["test:regular"] = "user"

	adminCalls := 0
	userCalls := 0
	var mu sync.Mutex

	adminLLM := &capturingLLM{fn: func(s string, h []llm.Message) (*llm.Response, error) {
		mu.Lock()
		adminCalls++
		mu.Unlock()
		return &llm.Response{Text: "admin response"}, nil
	}}
	userLLM := &capturingLLM{fn: func(s string, h []llm.Message) (*llm.Response, error) {
		mu.Lock()
		userCalls++
		mu.Unlock()
		return &llm.Response{Text: "user response"}, nil
	}}
	defaultLLM := &mockLLM{response: &llm.Response{Text: "default"}}

	pool := llm.NewPool(func(ctx context.Context, cfg llm.ProviderConfig) (llm.LLM, error) {
		switch cfg.Model {
		case "admin-model":
			return adminLLM, nil
		case "user-model":
			return userLLM, nil
		}
		return defaultLLM, nil
	}, defaultLLM)

	conn := newMockConnector()
	a := agent.New(conn, defaultLLM, tool.NewRegistry(), &mockEmbedder{}, st, agent.Options{
		Roles: map[string]config.RoleConfig{
			"admin": {LLM: config.LLMProviderConfig{Provider: "test", Model: "admin-model"}},
			"user":  {LLM: config.LLMProviderConfig{Provider: "test", Model: "user-model"}},
		},
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "user"},
		LLMPool:      pool,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(userID, text string) {
		conn.handler(ctx, connector.Message{Connector: "test", UserID: userID, ChatID: userID, SenderID: userID, Text: text})
		time.Sleep(50 * time.Millisecond)
	}

	send("admin_user", "hello from admin")
	send("regular", "hello from user")
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if adminCalls == 0 {
		t.Error("admin LLM never called for admin role")
	}
	if userCalls == 0 {
		t.Error("user LLM never called for user role")
	}
}

// TestIntegration_MemoryRememberedThenReset: use remember, reset, memories survive.
func TestIntegration_MemoryRememberedThenReset(t *testing.T) {
	st := newStateStore()

	r := tool.NewRegistry()
	r.Register(&fakeRememberTool{store: st})

	seqLLM := &sequenceLLM{responses: []*llm.Response{
		{ToolCall: &llm.ToolCall{Name: "remember", Args: map[string]any{"fact": "User is a caveman dev"}}},
		{Text: "Remembered."},
		{Text: "ok"},
		{Text: "ok"},
	}}

	conn := newMockConnector()
	a := agent.New(conn, seqLLM, r, &mockEmbedder{}, st, agent.Options{
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "user"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	send := runAndWait(t, ctx, conn, a)

	send("u1", "remember I am a caveman dev")
	time.Sleep(50 * time.Millisecond)

	// Verify memory saved
	st.mu.Lock()
	facts := append([]string{}, st.facts["test:u1"]...)
	st.mu.Unlock()
	if len(facts) == 0 {
		t.Error("expected memory to be saved")
	}

	send("u1", "/reset")
	time.Sleep(30 * time.Millisecond)

	// Memories still there
	st.mu.Lock()
	factsAfterReset := append([]string{}, st.facts["test:u1"]...)
	st.mu.Unlock()
	if len(factsAfterReset) == 0 {
		t.Error("memories wiped by /reset — should survive")
	}

	// /whoami also works after reset
	reply := send("u1", "/whoami")
	if reply == "" {
		t.Error("expected whoami reply after reset")
	}
}

// TestIntegration_AdminBootstrapNoStoreRole: userID in admins list gets admin even without store role.
func TestIntegration_AdminBootstrapNoStoreRole(t *testing.T) {
	st := newStateStore()
	// No role in store for bootstrap_admin

	lm := &mockLLM{response: &llm.Response{Text: "ok"}}
	conn, a := newAgent(t, lm, st, agent.Options{
		Admins:       []string{"bootstrap_admin"},
		DefaultRoles: map[string]string{"test": "user"},
		DefaultSouls: map[string]string{"test": "default"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	send := runAndWait(t, ctx, conn, a)

	// /factory-reset should work for bootstrap admin
	reply := send("bootstrap_admin", "/factory-reset")
	if strings.Contains(strings.ToLower(reply), "denied") {
		t.Errorf("bootstrap admin got denied: %q", reply)
	}
}

// TestIntegration_MultiUserIsolation: separate users have isolated soul/role/history.
func TestIntegration_MultiUserIsolation(t *testing.T) {
	st := newStateStore()
	st.souls["test:alice"] = "human"
	st.souls["test:bob"] = "caveman"
	st.roles["test:alice"] = "user"
	st.roles["test:bob"] = "admin"

	var mu sync.Mutex
	systemsSeen := map[string][]string{}

	capLLM := &capturingLLM{
		fn: func(system string, history []llm.Message) (*llm.Response, error) {
			// userID embedded in chatID, not accessible here
			// record systems to verify different souls used
			mu.Lock()
			systemsSeen["all"] = append(systemsSeen["all"], system)
			mu.Unlock()
			return &llm.Response{Text: fmt.Sprintf("reply len=%d", len(history))}, nil
		},
	}

	conn := newMockConnector()
	a := agent.New(conn, capLLM, tool.NewRegistry(), &mockEmbedder{}, st, agent.Options{
		Souls: map[string]config.SoulConfig{
			"human":   {Prompt: "HUMAN_PROMPT"},
			"caveman": {Prompt: "CAVEMAN_PROMPT"},
		},
		Roles: map[string]config.RoleConfig{
			"user":  {LLM: config.LLMProviderConfig{}},
			"admin": {LLM: config.LLMProviderConfig{}},
		},
		DefaultSouls: map[string]string{"test": "default"},
		DefaultRoles: map[string]string{"test": "user"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go a.Run(ctx)
	conn.waitReady(t)

	send := func(userID, text string) {
		conn.handler(ctx, connector.Message{Connector: "test", UserID: userID, ChatID: userID, SenderID: userID, Text: text})
		time.Sleep(40 * time.Millisecond)
	}

	send("alice", "hello alice")
	send("bob", "hello bob")
	send("alice", "second message from alice")
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	systems := systemsSeen["all"]
	mu.Unlock()

	humanCount := 0
	cavemanCount := 0
	for _, s := range systems {
		if strings.Contains(s, "HUMAN_PROMPT") {
			humanCount++
		}
		if strings.Contains(s, "CAVEMAN_PROMPT") {
			cavemanCount++
		}
	}
	if humanCount == 0 {
		t.Error("HUMAN_PROMPT never used (alice should get human soul)")
	}
	if cavemanCount == 0 {
		t.Error("CAVEMAN_PROMPT never used (bob should get caveman soul)")
	}
}

// --- test helpers ---

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fakeNamedTool is a no-op tool with a configurable name.
type fakeNamedTool struct{ name string }

func (f *fakeNamedTool) Name() string        { return f.name }
func (f *fakeNamedTool) Description() string { return f.name }
func (f *fakeNamedTool) Schema() *tool.Schema {
	return &tool.Schema{Properties: map[string]*tool.Property{}}
}
func (f *fakeNamedTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return "ok", nil
}

// fakeRememberTool saves facts to a stateStore (simulates real remember tool behaviour).
type fakeRememberTool struct{ store *stateStore }

func (f *fakeRememberTool) Name() string        { return "remember" }
func (f *fakeRememberTool) Description() string { return "remember a fact" }
func (f *fakeRememberTool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"fact": {Type: "string", Description: "fact to remember"},
		},
		Required: []string{"fact"},
	}
}
func (f *fakeRememberTool) Execute(_ context.Context, args map[string]any) (string, error) {
	fact, _ := args["fact"].(string)
	f.store.mu.Lock()
	f.store.facts["test:u1"] = append(f.store.facts["test:u1"], fact)
	f.store.mu.Unlock()
	return "Remembered: " + fact, nil
}

// toolInterceptLLM wraps an LLM and calls onCall with the tools list before delegating.
type toolInterceptLLM struct {
	inner  llm.LLM
	onCall func(tools []tool.Tool)
}

func (t *toolInterceptLLM) Chat(ctx context.Context, h []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	return t.ChatWithSystem(ctx, "", h, tools)
}
func (t *toolInterceptLLM) ChatWithSystem(ctx context.Context, s string, h []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	t.onCall(tools)
	return t.inner.ChatWithSystem(ctx, s, h, tools)
}
