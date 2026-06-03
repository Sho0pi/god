package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Registry holds tools by name and dispatches calls to them.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. A later registration with the same name replaces the
// earlier one.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Tools returns every registered tool.
func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// FilteredTools returns only tools whose names appear in allowed. An empty
// allowed list means all tools (mirrors a role's empty tool-list = full access).
func (r *Registry) FilteredTools(allowed []string) []Tool {
	if len(allowed) == 0 {
		return r.Tools()
	}
	out := make([]Tool, 0, len(allowed))
	for _, name := range allowed {
		if t, ok := r.tools[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Dispatch runs the named tool. args is the decoded argument map as provider
// SDKs hand it back; it is re-marshalled to JSON so the tool can decode into its
// own typed struct.
func (r *Registry) Dispatch(ctx context.Context, name string, args map[string]any) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return Result{}, fmt.Errorf("marshal args for %q: %w", name, err)
	}
	return t.Execute(ctx, raw)
}
