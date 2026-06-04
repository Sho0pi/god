package setup

import (
	"context"

	"github.com/sho0pi/god/internal/llm/gemini"
)

func init() { Register(geminiProvider{}) }

type geminiProvider struct{}

func (geminiProvider) Key() string    { return "gemini" }
func (geminiProvider) Title() string  { return "Gemini" }
func (geminiProvider) EnvVar() string { return "GEMINI_API_KEY" }

func (geminiProvider) Models() []string {
	return []string{"gemini-3.1-flash-lite", "gemini-3.1-flash", "gemini-3.1-pro"}
}

func (geminiProvider) ValidateKey(ctx context.Context, key string) error {
	return gemini.Validate(ctx, key)
}
