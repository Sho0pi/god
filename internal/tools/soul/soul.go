// Package soul provides the set_soul tool, which assigns a soul (personality)
// to the current user, persisted in the store. The next message from that user
// uses the assigned soul. Migrated to the provider-neutral internal/tools
// ecosystem; user identity arrives via tools.UserFrom(ctx).
package soul

import (
	"context"
	"fmt"
	"strings"

	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/tools"
)

// Args are the set_soul arguments.
type Args struct {
	Soul   string `json:"soul"`
	Reason string `json:"reason"`
}

// NewSetSoulTool returns the set_soul tool. knownSouls are the valid soul names
// (from config) offered to the model as an enum.
func NewSetSoulTool(s store.SoulStore, knownSouls []string) tools.Tool {
	return tools.NewTypedTool(
		"set_soul",
		"Assign a soul (personality) to the current user. Use this after learning "+
			"enough about the user to pick the right soul. Valid souls: "+
			strings.Join(knownSouls, ", ")+".",
		tools.Object(map[string]*tools.Property{
			"soul": {
				Type:        "string",
				Description: "Soul name to assign, e.g. 'human' or 'caveman'",
				Enum:        knownSouls,
			},
			"reason": {
				Type:        "string",
				Description: "Why this soul fits the user (one short sentence)",
			},
		}, "soul"),
		func(ctx context.Context, args Args) (tools.Result, error) {
			return setSoul(ctx, s, args)
		},
	)
}

func setSoul(ctx context.Context, s store.SoulStore, args Args) (tools.Result, error) {
	if args.Soul == "" {
		return tools.Result{}, fmt.Errorf("soul is required")
	}

	user, ok := tools.UserFrom(ctx)
	if !ok {
		return tools.Result{}, fmt.Errorf("no user context in request")
	}

	if err := s.AssignSoul(ctx, user.Connector, user.UserID, args.Soul); err != nil {
		return tools.Result{}, fmt.Errorf("assign soul: %w", err)
	}

	return tools.Result{
		Content: fmt.Sprintf("Soul set to %q for %s:%s", args.Soul, user.Connector, user.UserID),
		Data:    map[string]any{"soul": args.Soul, "connector": user.Connector, "user": user.UserID},
	}, nil
}
