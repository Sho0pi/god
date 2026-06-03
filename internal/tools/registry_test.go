package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func echoTool(name string) Tool {
	return NewTypedTool(name, "echo", Object(map[string]*Property{
		"v": {Type: "string"},
	}), func(_ context.Context, a struct {
		V string `json:"v"`
	}) (Result, error) {
		return Result{Content: a.V}, nil
	})
}

func TestRegistry_DispatchMarshalsMapArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(echoTool("echo"))

	res, err := r.Dispatch(context.Background(), "echo", map[string]any{"v": "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Content != "hi" {
		t.Fatalf("content = %q, want hi", res.Content)
	}
}

func TestRegistry_DispatchUnknownTool(t *testing.T) {
	r := NewRegistry()
	_, err := r.Dispatch(context.Background(), "nope", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistry_FilteredTools(t *testing.T) {
	r := NewRegistry()
	r.Register(echoTool("a"))
	r.Register(echoTool("b"))
	r.Register(echoTool("c"))

	if got := len(r.FilteredTools(nil)); got != 3 {
		t.Errorf("empty filter = %d tools, want 3 (all)", got)
	}
	got := r.FilteredTools([]string{"a", "c", "missing"})
	if len(got) != 2 {
		t.Fatalf("filtered = %d tools, want 2", len(got))
	}
}

func TestRegistry_RegisterReplaces(t *testing.T) {
	r := NewRegistry()
	r.Register(echoTool("dup"))
	r.Register(echoTool("dup"))
	if len(r.Tools()) != 1 {
		t.Fatalf("want 1 tool after duplicate register, got %d", len(r.Tools()))
	}
}

func TestRegistry_DispatchRoundTripsTypes(t *testing.T) {
	r := NewRegistry()
	r.Register(NewTypedTool("n", "num", Object(nil), func(_ context.Context, a struct {
		N json.Number `json:"n"`
	}) (Result, error) {
		return Result{Content: a.N.String()}, nil
	}))
	// A float from a decoded map must survive the map→json→struct round trip.
	res, err := r.Dispatch(context.Background(), "n", map[string]any{"n": 42})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "42" {
		t.Fatalf("content = %q, want 42", res.Content)
	}
}
