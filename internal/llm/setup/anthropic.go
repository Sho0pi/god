package setup

import (
	"context"

	"github.com/sho0pi/god/internal/llm/anthropic"
)

func init() { Register(anthropicProvider{}) }

type anthropicProvider struct{}

func (anthropicProvider) Key() string    { return "anthropic" }
func (anthropicProvider) Title() string  { return "Anthropic" }
func (anthropicProvider) EnvVar() string { return "ANTHROPIC_API_KEY" }

func (anthropicProvider) Models() []string {
	return []string{"claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5-20251001"}
}

func (anthropicProvider) ValidateKey(ctx context.Context, key string) error {
	return anthropic.Validate(ctx, key)
}
