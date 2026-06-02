package llm

import "context"

type LLM interface {
	Chat(ctx context.Context, message string) (string, error)
}
