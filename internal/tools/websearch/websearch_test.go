package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

func callTool(t *testing.T, tool tools.Tool, raw string) (tools.Result, error) {
	t.Helper()
	return tool.Execute(context.Background(), json.RawMessage(raw))
}

func TestWebSearch_HappyPath(t *testing.T) {
	var gotMax int
	tool := New(func(_ context.Context, q string, max int) (string, error) {
		gotMax = max
		return "1. Title — https://x.test", nil
	})
	res, err := callTool(t, tool, `{"query":"go frameworks"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "https://x.test") {
		t.Fatalf("content = %q", res.Content)
	}
	if gotMax != defaultResults {
		t.Fatalf("max = %d, want default %d", gotMax, defaultResults)
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	tool := New(func(_ context.Context, _ string, _ int) (string, error) { return "x", nil })
	if _, err := callTool(t, tool, `{"query":"   "}`); err == nil {
		t.Fatal("expected error for blank query")
	}
}

func TestWebSearch_MaxResultsClamped(t *testing.T) {
	var gotMax int
	tool := New(func(_ context.Context, _ string, max int) (string, error) {
		gotMax = max
		return "x", nil
	})
	if _, err := callTool(t, tool, `{"query":"q","max_results":999}`); err != nil {
		t.Fatal(err)
	}
	if gotMax != maxResults {
		t.Fatalf("max = %d, want clamp to %d", gotMax, maxResults)
	}
}

func TestWebSearch_NoResults(t *testing.T) {
	tool := New(func(_ context.Context, _ string, _ int) (string, error) { return "  \n ", nil })
	res, err := callTool(t, tool, `{"query":"obscure"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "No results") {
		t.Fatalf("content = %q, want no-results message", res.Content)
	}
}

func TestWebSearch_RunnerError(t *testing.T) {
	tool := New(func(_ context.Context, _ string, _ int) (string, error) {
		return "", errors.New("ddg down")
	})
	if _, err := callTool(t, tool, `{"query":"q"}`); err == nil {
		t.Fatal("expected runner error to propagate")
	}
}
