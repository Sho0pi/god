package openai

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

// serve spins an httptest server returning respJSON, capturing the request body.
func serve(t *testing.T, respJSON string, captured *map[string]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, captured)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respJSON)
	}))
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), "test-key", "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	c.baseURL = srv.URL
	return c
}

func TestChatReturnsText(t *testing.T) {
	c := serve(t, `{"choices":[{"message":{"role":"assistant","content":"hello there"}}]}`, nil)
	resp, err := c.ChatWithSystem(context.Background(), "sys", []llm.Message{{Role: "user", Text: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello there" {
		t.Errorf("text = %q, want %q", resp.Text, "hello there")
	}
	if resp.ToolCall != nil {
		t.Error("did not expect a tool call")
	}
}

func TestChatReturnsToolCall(t *testing.T) {
	resp := `{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}
	]}}]}`
	c := serve(t, resp, nil)
	got, err := c.ChatWithSystem(context.Background(), "", []llm.Message{{Role: "user", Text: "weather?"}}, []toolpkg.Tool{fakeTool{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolCall == nil {
		t.Fatal("expected a tool call")
	}
	if got.ToolCall.Name != "get_weather" {
		t.Errorf("name = %q", got.ToolCall.Name)
	}
	if got.ToolCall.Args["city"] != "NYC" {
		t.Errorf("args = %v", got.ToolCall.Args)
	}
	if string(got.ToolCall.ThoughtSignature) != "call_abc" {
		t.Errorf("tool-call id not round-tripped, got %q", got.ToolCall.ThoughtSignature)
	}
}

func TestRequestSerialization(t *testing.T) {
	var req map[string]any
	c := serve(t, `{"choices":[{"message":{"content":"ok"}}]}`, &req)

	history := []llm.Message{
		{Role: "user", Text: "weather in NYC?"},
		{Role: "model", ToolCall: &llm.ToolCall{Name: "get_weather", Args: map[string]any{"city": "NYC"}, ThoughtSignature: []byte("call_1")}},
		{ToolResult: &llm.ToolResult{Name: "get_weather", Result: "sunny", ThoughtSignature: []byte("call_1")}},
	}
	if _, err := c.ChatWithSystem(context.Background(), "be helpful", history, []toolpkg.Tool{fakeTool{}}); err != nil {
		t.Fatal(err)
	}

	if req["model"] != "gpt-test" {
		t.Errorf("model = %v", req["model"])
	}
	msgs := req["messages"].([]any)
	// system, user, assistant(tool_call), tool(result)
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d: %v", len(msgs), msgs)
	}
	if m0 := msgs[0].(map[string]any); m0["role"] != "system" || m0["content"] != "be helpful" {
		t.Errorf("first message should be system prompt, got %v", m0)
	}
	asst := msgs[2].(map[string]any)
	tcs := asst["tool_calls"].([]any)
	tc := tcs[0].(map[string]any)
	if tc["id"] != "call_1" {
		t.Errorf("assistant tool_call id = %v, want call_1", tc["id"])
	}
	toolMsg := msgs[3].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("tool result should reference call_1, got %v", toolMsg)
	}
	if _, ok := req["tools"]; !ok {
		t.Error("tools should be present in request")
	}
}

func TestAPIError(t *testing.T) {
	c := serve(t, `{"error":{"message":"invalid api key"}}`, nil)
	_, err := c.ChatWithSystem(context.Background(), "", []llm.Message{{Role: "user", Text: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestValidate(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ok.Close)
	if err := validateAt(context.Background(), ok.URL, "sk-key"); err != nil {
		t.Errorf("valid key should pass: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(bad.Close)
	if err := validateAt(context.Background(), bad.URL, "sk-key"); err == nil {
		t.Error("401 should fail validation")
	}

	if err := validateAt(context.Background(), ok.URL, ""); err == nil {
		t.Error("empty key should fail")
	}
}
