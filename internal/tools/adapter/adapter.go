// Package adapter converts a provider-neutral tools.Tool into each LLM
// provider's tool wire format. These are pure, client-free helpers: a provider
// client calls the matching function to build the tool list it sends to its
// API. The neutral tools.Schema serializes straight to JSON Schema, so the only
// per-provider difference is the envelope (where name/description live and the
// key the schema goes under).
package adapter

import "github.com/sho0pi/god/internal/tools"

// ToOpenAITool renders a tool in OpenAI's function-tool format.
func ToOpenAITool(t tools.Tool) map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        t.Name(),
		"description": t.Description(),
		"parameters":  t.Schema(),
	}
}

// ToClaudeTool renders a tool in Anthropic Claude's tool format.
func ToClaudeTool(t tools.Tool) map[string]any {
	return map[string]any{
		"name":         t.Name(),
		"description":  t.Description(),
		"input_schema": t.Schema(),
	}
}

// ToGeminiMap renders a tool in Gemini's function-declaration shape. Gemini's
// Go SDK prefers a typed genai.Schema (built in the gemini client); this map
// form exists for parity and for transports that take raw JSON.
func ToGeminiMap(t tools.Tool) map[string]any {
	return map[string]any{
		"name":        t.Name(),
		"description": t.Description(),
		"parameters":  t.Schema(),
	}
}
