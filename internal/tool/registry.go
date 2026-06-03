package tool

import (
	"context"
	"fmt"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// FilteredTools returns only the tools whose names appear in allowed.
// If allowed is empty, all tools are returned.
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

func (r *Registry) Dispatch(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.Execute(ctx, args)
}
