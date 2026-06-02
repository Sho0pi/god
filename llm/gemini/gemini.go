package gemini

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/tool"
)

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

func (c *Client) Chat(ctx context.Context, history []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	return c.ChatWithSystem(ctx, "", history, tools)
}

func (c *Client) ChatWithSystem(ctx context.Context, systemPrompt string, history []llm.Message, tools []tool.Tool) (*llm.Response, error) {
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

func toFuncDecls(tools []tool.Tool) []*genai.FunctionDeclaration {
	decls := make([]*genai.FunctionDeclaration, len(tools))
	for i, t := range tools {
		s := t.Schema()
		params := &genai.Schema{
			Type:       genai.TypeObject,
			Properties: make(map[string]*genai.Schema),
			Required:   s.Required,
		}
		for name, prop := range s.Properties {
			gs := &genai.Schema{Description: prop.Description}
			switch prop.Type {
			case "string":
				gs.Type = genai.TypeString
			case "number":
				gs.Type = genai.TypeNumber
			case "boolean":
				gs.Type = genai.TypeBoolean
			}
			if len(prop.Enum) > 0 {
				gs.Enum = prop.Enum
			}
			params.Properties[name] = gs
		}
		decls[i] = &genai.FunctionDeclaration{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  params,
		}
	}
	return decls
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
