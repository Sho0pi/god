// Package remind provides the remind tool: the model schedules a recurring
// instruction that god runs later and sends to the user's chat unprompted.
package remind

import (
	"context"
	"fmt"

	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/tools"
)

// Scheduler is the subset of the agent scheduler the tool needs.
type Scheduler interface {
	Add(ctx context.Context, r store.Reminder) (int64, error)
}

// Args are the remind arguments.
type Args struct {
	Schedule    string `json:"schedule"`
	Instruction string `json:"instruction"`
}

// New returns the remind tool backed by sched.
func New(sched Scheduler) tools.Tool {
	return tools.NewTypedTool(
		"remind",
		"Schedule a recurring reminder that runs later and is sent to this chat. "+
			"Use when the user asks to be reminded or to receive something on a schedule. "+
			"schedule is either a Go duration for 'every X' (e.g. '1m', '30m', '1h', '24h') "+
			"or a 5-field cron expression (e.g. '0 9 * * *' = 9am daily). instruction is what "+
			"god should do each time, phrased as a directive (e.g. 'Tell the user today's date.').",
		tools.Object(map[string]*tools.Property{
			"schedule": {
				Type:        "string",
				Description: "Go duration ('1m','1h') for every-X, or a cron expression ('0 9 * * *').",
			},
			"instruction": {
				Type:        "string",
				Description: "What to do each time, as a directive to god.",
			},
		}, "schedule", "instruction"),
		func(ctx context.Context, args Args) (tools.Result, error) {
			return run(ctx, sched, args)
		},
	)
}

func run(ctx context.Context, sched Scheduler, args Args) (tools.Result, error) {
	if args.Schedule == "" || args.Instruction == "" {
		return tools.Result{}, fmt.Errorf("schedule and instruction are required")
	}
	user, ok := tools.UserFrom(ctx)
	if !ok || user.ChatID == "" {
		return tools.Result{}, fmt.Errorf("no chat context for the reminder")
	}

	id, err := sched.Add(ctx, store.Reminder{
		Connector:   user.Connector,
		UserID:      user.UserID,
		ChatID:      user.ChatID,
		Schedule:    args.Schedule,
		Instruction: args.Instruction,
	})
	if err != nil {
		return tools.Result{}, fmt.Errorf("schedule reminder: %w", err)
	}
	return tools.Result{
		Content: fmt.Sprintf("Reminder #%d scheduled (%s). Manage with /reminders.", id, args.Schedule),
		Data:    map[string]any{"id": id, "schedule": args.Schedule},
	}, nil
}
