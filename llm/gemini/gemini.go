package gemini

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Client struct {
	inner *genai.Client
	model *genai.GenerativeModel
}

func New(ctx context.Context, apiKey, model string) (*Client, error) {
	c, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	return &Client{inner: c, model: c.GenerativeModel(model)}, nil
}

func (c *Client) Chat(ctx context.Context, message string) (string, error) {
	resp, err := c.model.GenerateContent(ctx, genai.Text(message))
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("empty response from gemini")
	}
	var out string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			out += string(t)
		}
	}
	return out, nil
}

func (c *Client) Close() error {
	return c.inner.Close()
}
