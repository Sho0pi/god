package soul

import (
	"context"
	"fmt"
	"strings"

	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/tool"
	"github.com/sho0pi/god/internal/tool/memory"
)

// SetSoulTool assigns a soul to the current user, persisted in the store.
// The next message from that user will use the assigned soul.
type SetSoulTool struct {
	store      store.SoulStore
	knownSouls []string // valid soul names from config
}

func NewSetSoulTool(s store.SoulStore, knownSouls []string) *SetSoulTool {
	return &SetSoulTool{store: s, knownSouls: knownSouls}
}

func (t *SetSoulTool) Name() string { return "set_soul" }

func (t *SetSoulTool) Description() string {
	return "Assign a soul (personality) to the current user. " +
		"Use this after learning enough about the user to pick the right soul. " +
		"Valid souls: " + strings.Join(t.knownSouls, ", ") + "."
}

func (t *SetSoulTool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"soul": {
				Type:        "string",
				Description: "Soul name to assign, e.g. 'human' or 'caveman'",
				Enum:        t.knownSouls,
			},
			"reason": {
				Type:        "string",
				Description: "Why this soul fits the user (one short sentence)",
			},
		},
		Required: []string{"soul"},
	}
}

func (t *SetSoulTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	soulName, _ := args["soul"].(string)
	if soulName == "" {
		return "", fmt.Errorf("soul is required")
	}

	user, ok := ctx.Value(memory.UserKey{}).(memory.UserInfo)
	if !ok || user.Connector == "" {
		return "", fmt.Errorf("no user context in request")
	}

	if err := t.store.AssignSoul(ctx, user.Connector, user.UserID, soulName); err != nil {
		return "", fmt.Errorf("assign soul: %w", err)
	}

	return fmt.Sprintf("Soul set to %q for %s:%s", soulName, user.Connector, user.UserID), nil
}
