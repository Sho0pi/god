package gemini

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"google.golang.org/genai"

	"github.com/sho0pi/god/internal/llm"
	toolpkg "github.com/sho0pi/god/internal/tools"
)

// validateBaseURL is the Gemini REST endpoint used by Validate. It's a var so
// tests can point it at an httptest server.
var validateBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Validate checks an API key with a cheap request (GET /models?key=…),
// returning nil if the key is accepted. Used by the `god model` setup wizard.
// It uses a raw HTTP call because the genai SDK has no lightweight ping.
func Validate(ctx context.Context, apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("gemini: API key is required")
	}
	u := validateBaseURL + "/models?key=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini: key rejected (status %d)", resp.StatusCode)
	}
	return nil
}

type Client struct {
	client *genai.Client
	model  string
}

func New(ctx context.Context, apiKey, model string) (*Client, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	return &Client{client: c, model: model}, nil
}

func (c *Client) ChatWithSystem(ctx context.Context, systemPrompt string, history []llm.Message, tools []toolpkg.Tool) (*llm.Response, error) {
	config := &genai.GenerateContentConfig{}

	if systemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: systemPrompt}},
		}
	}
	if len(tools) > 0 {
		config.Tools = []*genai.Tool{{FunctionDeclarations: toFuncDecls(tools)}}
	}

	contents := toContents(history)

	resp, err := c.client.Models.GenerateContent(ctx, c.model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("gemini send: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, fmt.Errorf("gemini: empty response")
	}

	for _, part := range resp.Candidates[0].Content.Parts {
		if part.FunctionCall != nil {
			return &llm.Response{ToolCall: &llm.ToolCall{
				Name:             part.FunctionCall.Name,
				Args:             part.FunctionCall.Args,
				ThoughtSignature: part.ThoughtSignature,
			}}, nil
		}
		if part.Text != "" {
			return &llm.Response{Text: part.Text}, nil
		}
	}

	return nil, fmt.Errorf("gemini: unrecognised response")
}

func (c *Client) Close() error { return nil }

func toFuncDecls(tools []toolpkg.Tool) []*genai.FunctionDeclaration {
	decls := make([]*genai.FunctionDeclaration, len(tools))
	for i, t := range tools {
		decls[i] = &genai.FunctionDeclaration{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  toGenaiSchema(t.Schema()),
		}
	}
	return decls
}

func toGenaiSchema(s *toolpkg.Schema) *genai.Schema {
	params := &genai.Schema{
		Type:       genai.TypeObject,
		Properties: make(map[string]*genai.Schema),
		Required:   s.Required,
	}
	for name, prop := range s.Properties {
		params.Properties[name] = toGenaiProperty(prop)
	}
	return params
}

func toGenaiProperty(prop *toolpkg.Property) *genai.Schema {
	gs := &genai.Schema{Description: prop.Description}
	switch prop.Type {
	case "string":
		gs.Type = genai.TypeString
	case "number", "integer":
		gs.Type = genai.TypeNumber
	case "boolean":
		gs.Type = genai.TypeBoolean
	case "object":
		gs.Type = genai.TypeObject
	case "array":
		gs.Type = genai.TypeArray
		if prop.Items != nil {
			gs.Items = toGenaiProperty(prop.Items)
		}
	}
	if len(prop.Enum) > 0 {
		gs.Enum = prop.Enum
	}
	return gs
}

func toContents(msgs []llm.Message) []*genai.Content {
	var out []*genai.Content
	for _, m := range msgs {
		switch {
		case m.ToolResult != nil:
			out = append(out, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{{
					FunctionResponse: &genai.FunctionResponse{
						Name:     m.ToolResult.Name,
						Response: map[string]any{"result": m.ToolResult.Result},
					},
					ThoughtSignature: m.ToolResult.ThoughtSignature,
				}},
			})
		case m.ToolCall != nil:
			out = append(out, &genai.Content{
				Role: "model",
				Parts: []*genai.Part{{
					FunctionCall: &genai.FunctionCall{
						Name: m.ToolCall.Name,
						Args: m.ToolCall.Args,
					},
					ThoughtSignature: m.ToolCall.ThoughtSignature,
				}},
			})
		case m.Role == "model":
			out = append(out, &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: m.Text}},
			})
		default:
			out = append(out, &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: m.Text}},
			})
		}
	}
	return out
}
