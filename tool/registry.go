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

func (r *Registry) Dispatch(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.Execute(ctx, args)
}
