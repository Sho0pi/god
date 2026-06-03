package adapter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

func sampleTool() tools.Tool {
	return tools.NewTypedTool("get_weather", "Get the weather.",
		tools.Object(map[string]*tools.Property{
			"city": {Type: "string", Description: "City name."},
		}, "city"),
		func(_ context.Context, _ struct{}) (tools.Result, error) { return tools.Result{}, nil })
}

func TestToOpenAITool(t *testing.T) {
	m := ToOpenAITool(sampleTool())
	if m["type"] != "function" {
		t.Errorf("type = %v, want function", m["type"])
	}
	if m["name"] != "get_weather" {
		t.Errorf("name = %v", m["name"])
	}
	if _, ok := m["parameters"].(*tools.Schema); !ok {
		t.Errorf("parameters should be *tools.Schema, got %T", m["parameters"])
	}
}

func TestToClaudeTool(t *testing.T) {
	m := ToClaudeTool(sampleTool())
	if _, ok := m["input_schema"]; !ok {
		t.Error("claude tool must use input_schema key")
	}
	if _, ok := m["parameters"]; ok {
		t.Error("claude tool must not use parameters key")
	}
}

func TestAdapters_ProduceValidJSON(t *testing.T) {
	for name, m := range map[string]map[string]any{
		"openai": ToOpenAITool(sampleTool()),
		"claude": ToClaudeTool(sampleTool()),
		"gemini": ToGeminiMap(sampleTool()),
	} {
		b, err := json.Marshal(m)
		if err != nil {
			t.Errorf("%s: marshal failed: %v", name, err)
			continue
		}
		if !json.Valid(b) {
			t.Errorf("%s: invalid json", name)
		}
	}
}
