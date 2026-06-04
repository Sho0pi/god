package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sho0pi/god/internal/llm"
	toolpkg "github.com/sho0pi/god/internal/tools"
)

type fakeTool struct{}

func (fakeTool) Name() string        { return "get_weather" }
func (fakeTool) Description() string { return "Get the weather for a city" }
func (fakeTool) Schema() *toolpkg.Schema {
	return toolpkg.Object(map[string]*toolpkg.Property{"city": {Type: "string"}}, "city")
}
func (fakeTool) Execute(context.Context, json.RawMessage) (toolpkg.Result, error) {
	return toolpkg.Result{}, nil
}

func serve(t *testing.T, respJSON string, captured *map[string]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		if captured != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, captured)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respJSON)
	}))
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), "test-key", "claude-test")
	if err != nil {
		t.Fatal(err)
	}
	c.baseURL = srv.URL
	return c
}

func TestChatReturnsText(t *testing.T) {
	c := serve(t, `{"content":[{"type":"text","text":"hello there"}]}`, nil)
	resp, err := c.ChatWithSystem(context.Background(), "sys", []llm.Message{{Role: "user", Text: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello there" {
		t.Errorf("text = %q", resp.Text)
	}
}

func TestChatReturnsToolCall(t *testing.T) {
	resp := `{"content":[
		{"type":"text","text":"let me check"},
		{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"NYC"}}
	]}`
	c := serve(t, resp, nil)
	got, err := c.ChatWithSystem(context.Background(), "", []llm.Message{{Role: "user", Text: "weather?"}}, []toolpkg.Tool{fakeTool{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolCall == nil {
		t.Fatal("expected a tool call")
	}
	if got.ToolCall.Name != "get_weather" || got.ToolCall.Args["city"] != "NYC" {
		t.Errorf("tool call = %+v", got.ToolCall)
	}
	if string(got.ToolCall.ThoughtSignature) != "toolu_1" {
		t.Errorf("tool_use id not round-tripped, got %q", got.ToolCall.ThoughtSignature)
	}
}

func TestRequestSerialization(t *testing.T) {
	var req map[string]any
	c := serve(t, `{"content":[{"type":"text","text":"ok"}]}`, &req)

	history := []llm.Message{
		{Role: "user", Text: "weather in NYC?"},
		{Role: "model", ToolCall: &llm.ToolCall{Name: "get_weather", Args: map[string]any{"city": "NYC"}, ThoughtSignature: []byte("toolu_1")}},
		{ToolResult: &llm.ToolResult{Name: "get_weather", Result: "sunny", ThoughtSignature: []byte("toolu_1")}},
	}
	if _, err := c.ChatWithSystem(context.Background(), "be helpful", history, []toolpkg.Tool{fakeTool{}}); err != nil {
		t.Fatal(err)
	}

	// system is a top-level field, not a message.
	if req["system"] != "be helpful" {
		t.Errorf("system = %v", req["system"])
	}
	if _, ok := req["max_tokens"]; !ok {
		t.Error("max_tokens is required and must be present")
	}
	msgs := req["messages"].([]any)
	if len(msgs) != 3 { // user, assistant(tool_use), user(tool_result)
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	asst := msgs[1].(map[string]any)
	useBlock := asst["content"].([]any)[0].(map[string]any)
	if useBlock["type"] != "tool_use" || useBlock["id"] != "toolu_1" {
		t.Errorf("assistant tool_use block = %v", useBlock)
	}
	resBlock := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if resBlock["type"] != "tool_result" || resBlock["tool_use_id"] != "toolu_1" {
		t.Errorf("tool_result block = %v", resBlock)
	}
}

func TestAPIError(t *testing.T) {
	c := serve(t, `{"error":{"message":"overloaded"}}`, nil)
	_, err := c.ChatWithSystem(context.Background(), "", []llm.Message{{Role: "user", Text: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(context.Background(), "", "claude-test"); err == nil {
		t.Error("expected error for empty api key")
	}
}

func TestValidate(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" || r.Header.Get("anthropic-version") == "" {
			t.Error("missing auth/version headers")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ok.Close)
	if err := validateAt(context.Background(), ok.URL, "key"); err != nil {
		t.Errorf("valid key should pass: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(bad.Close)
	if err := validateAt(context.Background(), bad.URL, "key"); err == nil {
		t.Error("401 should fail validation")
	}
}
