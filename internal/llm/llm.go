package llm

import (
	"context"

	"github.com/sho0pi/god/internal/tool"
)

type ToolCall struct {
	Name             string
	Args             map[string]any
	ThoughtSignature []byte // round-tripped for thinking-enabled models
}

type ToolResult struct {
	Name             string
	Result           string
	ThoughtSignature []byte // copied from ToolCall when dispatching
}

type Message struct {
	Role       string      // "user" | "model"
	Text       string      // for user/model text turns
	ToolCall   *ToolCall   // set on model turn when a tool was requested
	ToolResult *ToolResult // set on tool result turn
}

type Response struct {
	Text     string    // non-empty on final answer
	ToolCall *ToolCall // non-empty when tool was requested
}

type LLM interface {
	ChatWithSystem(ctx context.Context, systemPrompt string, history []Message, tools []tool.Tool) (*Response, error)
}
