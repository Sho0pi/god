// Package openai is an llm.LLM adapter for OpenAI's Chat Completions API. It is
// a thin hand-rolled HTTP client (no SDK) covering exactly what the agent needs:
// a system prompt, a message history with tool calls/results, and function
// (tool) calling. One response is either a final text answer or a single tool
// call, matching the agent's tool loop.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sho0pi/god/internal/llm"
	toolpkg "github.com/sho0pi/god/internal/tools"
)

const defaultBaseURL = "https://api.openai.com/v1"

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// New creates an OpenAI client for the given API key and model.
func New(_ context.Context, apiKey, model string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai: API key is required")
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (c *Client) Close() error { return nil }

// --- wire types ------------------------------------------------------------

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFuncCall `json:"function"`
}

type chatFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded object, per the API
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFuncDecl `json:"function"`
}

type chatFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  *toolpkg.Schema `json:"parameters"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// --- adapter ---------------------------------------------------------------

func (c *Client) ChatWithSystem(ctx context.Context, systemPrompt string, history []llm.Message, tools []toolpkg.Tool) (*llm.Response, error) {
	reqBody := chatRequest{Model: c.model, Messages: toMessages(systemPrompt, history)}
	for _, t := range tools {
		reqBody.Tools = append(reqBody.Tools, chatTool{
			Type:     "function",
			Function: chatFuncDecl{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()},
		})
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("openai: decode response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("openai: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty response")
	}

	msg := parsed.Choices[0].Message
	if len(msg.ToolCalls) > 0 {
		tc := msg.ToolCalls[0]
		args := map[string]any{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("openai: decode tool args: %w", err)
			}
		}
		return &llm.Response{ToolCall: &llm.ToolCall{
			Name: tc.Function.Name,
			Args: args,
			// Round-trip the tool-call id through ThoughtSignature so the
			// follow-up tool result can reference it (OpenAI requires it).
			ThoughtSignature: []byte(tc.ID),
		}}, nil
	}
	return &llm.Response{Text: msg.Content}, nil
}

// toMessages converts the system prompt + history into OpenAI's message list.
func toMessages(systemPrompt string, history []llm.Message) []chatMessage {
	var out []chatMessage
	if systemPrompt != "" {
		out = append(out, chatMessage{Role: "system", Content: systemPrompt})
	}
	for _, m := range history {
		switch {
		case m.ToolResult != nil:
			out = append(out, chatMessage{
				Role:       "tool",
				ToolCallID: string(m.ToolResult.ThoughtSignature),
				Content:    m.ToolResult.Result,
			})
		case m.ToolCall != nil:
			args, _ := json.Marshal(m.ToolCall.Args)
			out = append(out, chatMessage{
				Role: "assistant",
				ToolCalls: []chatToolCall{{
					ID:       string(m.ToolCall.ThoughtSignature),
					Type:     "function",
					Function: chatFuncCall{Name: m.ToolCall.Name, Arguments: string(args)},
				}},
			})
		case m.Role == "model":
			out = append(out, chatMessage{Role: "assistant", Content: m.Text})
		default:
			out = append(out, chatMessage{Role: "user", Content: m.Text})
		}
	}
	return out
}
