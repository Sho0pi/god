// Package anthropic is an llm.LLM adapter for Anthropic's Messages API. Like the
// openai package it is a thin hand-rolled HTTP client covering a system prompt,
// a message history with tool use/results, and tool calling. One response is
// either a final text answer or a single tool call.
package anthropic

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

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	apiVersion     = "2023-06-01"
	// maxTokens caps the response length; the Messages API requires it.
	maxTokens = 4096
)

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// New creates an Anthropic client for the given API key and model.
func New(_ context.Context, apiKey, model string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: API key is required")
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

type msgRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
	Tools     []tool    `json:"tools,omitempty"`
}

type message struct {
	Role    string  `json:"role"` // "user" | "assistant"
	Content []block `json:"content"`
}

// block is a content block. Only the fields relevant to the active Type are set.
type block struct {
	Type string `json:"type"` // "text" | "tool_use" | "tool_result"

	Text string `json:"text,omitempty"` // text

	ID    string         `json:"id,omitempty"`    // tool_use
	Name  string         `json:"name,omitempty"`  // tool_use
	Input map[string]any `json:"input,omitempty"` // tool_use

	ToolUseID string `json:"tool_use_id,omitempty"` // tool_result
	Content   string `json:"content,omitempty"`     // tool_result
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema *toolpkg.Schema `json:"input_schema"`
}

type msgResponse struct {
	Content []block `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// --- adapter ---------------------------------------------------------------

func (c *Client) ChatWithSystem(ctx context.Context, systemPrompt string, history []llm.Message, tools []toolpkg.Tool) (*llm.Response, error) {
	reqBody := msgRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  toMessages(history),
	}
	for _, t := range tools {
		reqBody.Tools = append(reqBody.Tools, tool{
			Name: t.Name(), Description: t.Description(), InputSchema: t.Schema(),
		})
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed msgResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decode response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("anthropic: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	// A response may interleave text and tool_use blocks; prefer a tool call.
	var text string
	for _, b := range parsed.Content {
		switch b.Type {
		case "tool_use":
			return &llm.Response{ToolCall: &llm.ToolCall{
				Name: b.Name,
				Args: b.Input,
				// Round-trip the tool_use id via ThoughtSignature so the
				// tool result can reference it.
				ThoughtSignature: []byte(b.ID),
			}}, nil
		case "text":
			text += b.Text
		}
	}
	return &llm.Response{Text: text}, nil
}

// toMessages converts history into Anthropic's user/assistant block messages.
func toMessages(history []llm.Message) []message {
	var out []message
	for _, m := range history {
		switch {
		case m.ToolResult != nil:
			out = append(out, message{Role: "user", Content: []block{{
				Type:      "tool_result",
				ToolUseID: string(m.ToolResult.ThoughtSignature),
				Content:   m.ToolResult.Result,
			}}})
		case m.ToolCall != nil:
			out = append(out, message{Role: "assistant", Content: []block{{
				Type:  "tool_use",
				ID:    string(m.ToolCall.ThoughtSignature),
				Name:  m.ToolCall.Name,
				Input: m.ToolCall.Args,
			}}})
		case m.Role == "model":
			out = append(out, message{Role: "assistant", Content: []block{{Type: "text", Text: m.Text}}})
		default:
			out = append(out, message{Role: "user", Content: []block{{Type: "text", Text: m.Text}}})
		}
	}
	return out
}
