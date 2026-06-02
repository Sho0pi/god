package tool

import "context"

type Property struct {
	Type        string   // "string", "number", "boolean"
	Description string
	Enum        []string // optional
}

type Schema struct {
	Properties map[string]*Property
	Required   []string
}

type Tool interface {
	Name()        string
	Description() string
	Schema()      *Schema
	Execute(ctx context.Context, args map[string]any) (string, error)
}
