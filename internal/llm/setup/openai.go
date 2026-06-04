package setup

import (
	"context"

	"github.com/sho0pi/god/internal/llm/openai"
)

func init() { Register(openaiProvider{}) }

type openaiProvider struct{}

func (openaiProvider) Key() string    { return "openai" }
func (openaiProvider) Title() string  { return "OpenAI" }
func (openaiProvider) EnvVar() string { return "OPENAI_API_KEY" }

func (openaiProvider) Models() []string {
	return []string{"gpt-4o-mini", "gpt-4o", "o4-mini"}
}

func (openaiProvider) ValidateKey(ctx context.Context, key string) error {
	return openai.Validate(ctx, key)
}
