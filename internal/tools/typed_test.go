package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type addArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

func newAddTool() Tool {
	return NewTypedTool("add", "adds a and b", Object(map[string]*Property{
		"a": {Type: "number"},
		"b": {Type: "number"},
	}, "a", "b"), func(_ context.Context, args addArgs) (Result, error) {
		return Result{Content: "ok", Data: map[string]any{"sum": args.A + args.B}}, nil
	})
}

func TestTypedTool_Execute_DecodesArgs(t *testing.T) {
	tool := newAddTool()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"a":2,"b":3}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Data["sum"] != 5 {
		t.Fatalf("sum = %v, want 5", res.Data["sum"])
	}
}

func TestTypedTool_Execute_EmptyPayloadIsZeroValue(t *testing.T) {
	tool := newAddTool()
	for _, raw := range []json.RawMessage{nil, json.RawMessage("")} {
		res, err := tool.Execute(context.Background(), raw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", raw, err)
		}
		if res.Data["sum"] != 0 {
			t.Fatalf("sum = %v, want 0 (zero value)", res.Data["sum"])
		}
	}
}

func TestTypedTool_Execute_MalformedJSON(t *testing.T) {
	tool := newAddTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"a":`))
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "add") {
		t.Fatalf("error should name the tool, got: %v", err)
	}
}

func TestTypedTool_Execute_PropagatesHandlerError(t *testing.T) {
	sentinel := errors.New("boom")
	tool := NewTypedTool("fail", "always fails", Object(nil),
		func(_ context.Context, _ struct{}) (Result, error) {
			return Result{}, sentinel
		})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want sentinel", err)
	}
}

func TestObject_Defaults(t *testing.T) {
	s := Object(map[string]*Property{"x": {Type: "string"}}, "x")
	if s.Type != "object" {
		t.Errorf("Type = %q, want object", s.Type)
	}
	if s.AdditionalProperties {
		t.Error("AdditionalProperties should default false")
	}
	if len(s.Required) != 1 || s.Required[0] != "x" {
		t.Errorf("Required = %v, want [x]", s.Required)
	}
}

func TestSchema_SerializesToJSONSchema(t *testing.T) {
	s := Object(map[string]*Property{
		"city": {Type: "string", Description: "a city"},
		"unit": {Type: "string", Enum: []string{"c", "f"}},
	}, "city")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"type":"object"`, `"required":["city"]`, `"additionalProperties":false`, `"enum":["c","f"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("marshalled schema missing %s\ngot: %s", want, got)
		}
	}
}
