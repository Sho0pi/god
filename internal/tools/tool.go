// Package tools is the provider-neutral tool ecosystem. A tool exposes a name,
// description, and JSON schema to the model; the model returns a tool call; the
// agent dispatches it by name and feeds the Result back. Nothing in a tool
// knows which LLM provider is in use — provider adapters (see the adapter
// subpackage) translate the neutral Schema into each provider's wire format.
package tools

import (
	"context"
	"encoding/json"
)

// Result is what a tool returns. Content is the text fed back to the model;
// Data carries optional structured payload for callers that want it (the agent
// loop currently uses Content only).
type Result struct {
	Content string         `json:"content"`
	Data    map[string]any `json:"data,omitempty"`
}

// Tool is the provider-neutral interface every tool implements. Execute
// receives the raw JSON arguments the model produced so each tool decodes into
// its own typed struct (see TypedTool, which does this for you).
type Tool interface {
	Name() string
	Description() string
	Schema() *Schema
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// Property describes a single JSON-schema object property. The json tags make
// Schema serialize directly to standard JSON Schema, which is what the OpenAI
// and Claude adapters hand to their APIs verbatim.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	// Items describes element type when Type == "array".
	Items *Property `json:"items,omitempty"`
}

// Schema is a JSON-schema object describing a tool's arguments.
type Schema struct {
	Type                 string               `json:"type"`
	Properties           map[string]*Property `json:"properties"`
	Required             []string             `json:"required,omitempty"`
	AdditionalProperties bool                 `json:"additionalProperties"`
}

// Object builds an object Schema with AdditionalProperties disabled (the
// stricter, recommended default for tool arguments).
func Object(props map[string]*Property, required ...string) *Schema {
	return &Schema{
		Type:                 "object",
		Properties:           props,
		Required:             required,
		AdditionalProperties: false,
	}
}
