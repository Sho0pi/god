package gemini

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/tool"
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

func (c *Client) Chat(ctx context.Context, history []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	return c.ChatWithSystem(ctx, "", history, tools)
}

func (c *Client) ChatWithSystem(ctx context.Context, systemPrompt string, history []llm.Message, tools []tool.Tool) (*llm.Response, error) {
	if systemPrompt != "" {
		c.model.SystemInstruction = &genai.Content{Parts: []genai.Part{genai.Text(systemPrompt)}}
	} else {
		c.model.SystemInstruction = nil
	}
	c.model.Tools = toGeminiTools(tools)

	session := c.model.StartChat()

	// All messages except the last go into history.
	if len(history) > 1 {
		session.History = toGeminiHistory(history[:len(history)-1])
	}

	last := history[len(history)-1]
	part, err := toGeminiPart(last)
	if err != nil {
		return nil, err
	}

	resp, err := session.SendMessage(ctx, part)
	if err != nil {
		return nil, fmt.Errorf("gemini send: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, fmt.Errorf("gemini: empty response")
	}

	for _, p := range resp.Candidates[0].Content.Parts {
		switch v := p.(type) {
		case genai.FunctionCall:
			return &llm.Response{ToolCall: &llm.ToolCall{Name: v.Name, Args: v.Args}}, nil
		case genai.Text:
			if s := string(v); s != "" {
				return &llm.Response{Text: s}, nil
			}
		}
	}

	return nil, fmt.Errorf("gemini: unrecognised response")
}

func (c *Client) Close() error { return c.inner.Close() }

// toGeminiTools converts our tool definitions to genai format.
func toGeminiTools(tools []tool.Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]*genai.FunctionDeclaration, len(tools))
	for i, t := range tools {
		decls[i] = toFuncDecl(t)
	}
	return []*genai.Tool{{FunctionDeclarations: decls}}
}

func toFuncDecl(t tool.Tool) *genai.FunctionDeclaration {
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
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  params,
	}
}

// toGeminiHistory converts our message slice to genai chat history.
func toGeminiHistory(msgs []llm.Message) []*genai.Content {
	var out []*genai.Content
	for _, m := range msgs {
		switch {
		case m.ToolResult != nil:
			out = append(out, &genai.Content{
				Role: "function",
				Parts: []genai.Part{genai.FunctionResponse{
					Name:     m.ToolResult.Name,
					Response: map[string]any{"result": m.ToolResult.Result},
				}},
			})
		case m.ToolCall != nil:
			out = append(out, &genai.Content{
				Role:  "model",
				Parts: []genai.Part{genai.FunctionCall{Name: m.ToolCall.Name, Args: m.ToolCall.Args}},
			})
		case m.Role == "model":
			out = append(out, &genai.Content{
				Role:  "model",
				Parts: []genai.Part{genai.Text(m.Text)},
			})
		default: // "user"
			out = append(out, &genai.Content{
				Role:  "user",
				Parts: []genai.Part{genai.Text(m.Text)},
			})
		}
	}
	return out
}

// toGeminiPart converts the last message to the part we send to the session.
func toGeminiPart(m llm.Message) (genai.Part, error) {
	if m.ToolResult != nil {
		return genai.FunctionResponse{
			Name:     m.ToolResult.Name,
			Response: map[string]any{"result": m.ToolResult.Result},
		}, nil
	}
	return genai.Text(m.Text), nil
}
