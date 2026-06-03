// Package memory provides the remember tool, which writes a fact about the user
// to long-term (pgvector) memory. Migrated to the provider-neutral
// internal/tools ecosystem; user identity arrives via tools.UserFrom(ctx).
package memory

import (
	"context"
	"fmt"

	"github.com/sho0pi/god/internal/embed"
	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/tools"
)

// Args are the remember arguments.
type Args struct {
	Fact string `json:"fact"`
}

// NewRememberTool returns the remember tool. It embeds the fact and saves it to
// the requesting user's long-term memory.
func NewRememberTool(e embed.Embedder, s store.MemoryStore) tools.Tool {
	return tools.NewTypedTool(
		"remember",
		"Save an important fact about this user or conversation to long-term memory. "+
			"Use this when the user shares something worth remembering across sessions: "+
			"preferences, background, ongoing projects, or important context.",
		tools.Object(map[string]*tools.Property{
			"fact": {
				Type:        "string",
				Description: "The fact to remember, as a concise statement. E.g. 'User prefers TypeScript over JavaScript'",
			},
		}, "fact"),
		func(ctx context.Context, args Args) (tools.Result, error) {
			return remember(ctx, e, s, args)
		},
	)
}

func remember(ctx context.Context, e embed.Embedder, s store.MemoryStore, args Args) (tools.Result, error) {
	if args.Fact == "" {
		return tools.Result{}, fmt.Errorf("fact is required")
	}

	user, ok := tools.UserFrom(ctx)
	if !ok {
		return tools.Result{}, fmt.Errorf("no user context in request")
	}

	embedding, err := e.Embed(ctx, args.Fact)
	if err != nil {
		return tools.Result{}, fmt.Errorf("embed fact: %w", err)
	}
	if err := s.SaveMemory(ctx, user.Connector, user.UserID, args.Fact, embedding); err != nil {
		return tools.Result{}, fmt.Errorf("save memory: %w", err)
	}

	return tools.Result{
		Content: "Remembered: " + args.Fact,
		Data:    map[string]any{"fact": args.Fact},
	}, nil
}
