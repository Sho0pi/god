package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// HandlerFunc is a tool body that works on already-decoded, typed arguments.
type HandlerFunc[T any] func(ctx context.Context, args T) (Result, error)

// TypedTool adapts a typed handler to the Tool interface, removing the
// repeated json.Unmarshal boilerplate from every tool. T is the tool's
// argument struct (with json tags matching its Schema).
type TypedTool[T any] struct {
	name        string
	description string
	schema      *Schema
	handler     HandlerFunc[T]
}

// NewTypedTool builds a Tool from a typed handler. The handler may be a closure
// capturing dependencies (stores, clients, config) the tool needs.
func NewTypedTool[T any](name, description string, schema *Schema, handler HandlerFunc[T]) *TypedTool[T] {
	return &TypedTool[T]{name: name, description: description, schema: schema, handler: handler}
}

func (t *TypedTool[T]) Name() string        { return t.name }
func (t *TypedTool[T]) Description() string { return t.description }
func (t *TypedTool[T]) Schema() *Schema     { return t.schema }

// Execute decodes raw into T and runs the handler. A nil/empty payload decodes
// into the zero value of T so tools with all-optional args still run.
func (t *TypedTool[T]) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args T
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Result{}, fmt.Errorf("%s: decode args: %w", t.name, err)
		}
	}
	return t.handler(ctx, args)
}
