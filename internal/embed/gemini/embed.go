package gemini

import (
	"context"
	"fmt"

	"google.golang.org/genai"
)

const defaultModel = "gemini-embedding-001"

type Embedder struct {
	client *genai.Client
	model  string
}

func New(ctx context.Context, apiKey string) (*Embedder, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	return &Embedder{client: c, model: defaultModel}, nil
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	contents := []*genai.Content{
		{Parts: []*genai.Part{{Text: text}}},
	}
	resp, err := e.client.Models.EmbedContent(ctx, e.model, contents, nil)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	return resp.Embeddings[0].Values, nil
}
