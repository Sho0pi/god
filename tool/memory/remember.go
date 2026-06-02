package memory

import (
	"context"
	"fmt"

	"github.com/sho0pi/god/embed"
	"github.com/sho0pi/god/store"
	"github.com/sho0pi/god/tool"
)

// UserKey is used to pass user identity through context to tools.
type UserKey struct{}

type UserInfo struct {
	Connector string
	UserID    string
}

// RememberTool saves a fact about the user into long-term memory.
type RememberTool struct {
	embedder embed.Embedder
	store    store.Store
}

func NewRememberTool(e embed.Embedder, s store.Store) *RememberTool {
	return &RememberTool{embedder: e, store: s}
}

func (t *RememberTool) Name() string { return "remember" }

func (t *RememberTool) Description() string {
	return "Save an important fact about this user or conversation to long-term memory. " +
		"Use this when the user shares something worth remembering across sessions: " +
		"preferences, background, ongoing projects, or important context."
}

func (t *RememberTool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"fact": {
				Type:        "string",
				Description: "The fact to remember, written as a concise statement. E.g. 'User prefers TypeScript over JavaScript'",
			},
		},
		Required: []string{"fact"},
	}
}

func (t *RememberTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	fact, _ := args["fact"].(string)
	if fact == "" {
		return "", fmt.Errorf("fact is required")
	}

	user, ok := ctx.Value(UserKey{}).(UserInfo)
	if !ok || user.Connector == "" {
		return "", fmt.Errorf("no user context in request")
	}

	embedding, err := t.embedder.Embed(ctx, fact)
	if err != nil {
		return "", fmt.Errorf("embed fact: %w", err)
	}

	if err := t.store.SaveMemory(ctx, user.Connector, user.UserID, fact, embedding); err != nil {
		return "", fmt.Errorf("save memory: %w", err)
	}

	return fmt.Sprintf("Remembered: %s", fact), nil
}
