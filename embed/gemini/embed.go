package gemini

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

const defaultModel = "text-embedding-004"

type Embedder struct {
	model *genai.EmbeddingModel
}

func New(ctx context.Context, apiKey string) (*Embedder, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	return &Embedder{model: client.EmbeddingModel(defaultModel)}, nil
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	res, err := e.model.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	return res.Embedding.Values, nil
}
