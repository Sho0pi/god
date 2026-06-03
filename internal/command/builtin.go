package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Builtin returns the default set of slash commands.
func Builtin() []Definition {
	return []Definition{
		resetCommand(),
		factoryResetCommand(),
		whoamiCommand(),
		allowCommand(),
		approveCommand(),
		denyCommand(),
	}
}

func approveCommand() Definition {
	return Definition{
		Name:        "approve",
		Description: "Admin only. Approve a pending action god requested.",
		Usage:       "/approve <id>",
		Handler:     approvalHandler(true),
	}
}

func denyCommand() Definition {
	return Definition{
		Name:        "deny",
		Description: "Admin only. Reject a pending action god requested.",
		Usage:       "/deny <id>",
		Handler:     approvalHandler(false),
	}
}

func approvalHandler(approve bool) func(context.Context, Request, Runtime) error {
	verb := "approve"
	if !approve {
		verb = "deny"
	}
	return func(_ context.Context, req Request, rt Runtime) error {
		if rt == nil || !rt.IsAdmin() {
			return req.Reply("Permission denied.")
		}
		fields := strings.Fields(req.Text)
		if len(fields) < 2 {
			return req.Reply("Usage: /" + verb + " <id>")
		}
		rt.ResolveApproval(approve, fields[1]) // sends its own replies
		return nil
	}
}

func allowCommand() Definition {
	return Definition{
		Name:        "allow",
		Description: "Admin only. Manage the WhatsApp allow list. Add/remove numbers or list them.",
		Usage:       "/allow add <number> | /allow remove <number> | /allow list",
		Handler: func(_ context.Context, req Request, rt Runtime) error {
			if rt == nil || !rt.IsAdmin() {
				return req.Reply("Permission denied.")
			}

			fields := strings.Fields(req.Text) // ["/allow", "add", "<number>"]
			if len(fields) < 2 {
				return req.Reply("Usage: /allow add <number> | /allow remove <number> | /allow list")
			}
			sub := strings.ToLower(fields[1])

			switch sub {
			case "list":
				nums, err := rt.AllowList()
				if errors.Is(err, ErrUnsupported) {
					return req.Reply("Allow list unavailable (no store configured).")
				}
				if err != nil {
					return req.Reply("Failed to list: " + err.Error())
				}
				if len(nums) == 0 {
					return req.Reply("Allow list is empty (stored). Note: an empty list means all senders are accepted.")
				}
				return req.Reply("Allowed numbers:\n" + strings.Join(nums, "\n"))
			case "add", "remove":
				if len(fields) < 3 {
					return req.Reply("Usage: /allow " + sub + " <number>")
				}
				number := fields[2]
				var err error
				if sub == "add" {
					err = rt.AllowAdd(number)
				} else {
					err = rt.AllowRemove(number)
				}
				if errors.Is(err, ErrUnsupported) {
					return req.Reply("Allow list unavailable (no store configured).")
				}
				if err != nil {
					return req.Reply("Failed to " + sub + ": " + err.Error())
				}
				if sub == "add" {
					return req.Reply("Added " + number + " to the allow list.")
				}
				return req.Reply("Removed " + number + " from the allow list.")
			default:
				return req.Reply("Unknown subcommand. Usage: /allow add <number> | /allow remove <number> | /allow list")
			}
		},
	}
}

func resetCommand() Definition {
	return Definition{
		Name:        "reset",
		Description: "Clear conversation history. Soul, role, and memories are kept.",
		Usage:       "/reset",
		Handler: func(_ context.Context, req Request, rt Runtime) error {
			if rt == nil {
				return req.Reply("Reset unavailable.")
			}
			if err := rt.ClearHistory(); err != nil {
				return req.Reply("Failed to reset: " + err.Error())
			}
			return req.Reply("Conversation cleared. Your soul, role, and memories are kept.")
		},
	}
}

func factoryResetCommand() Definition {
	return Definition{
		Name:        "factory-reset",
		Description: "Admin only. Wipes soul, role, all memories, and conversation history.",
		Usage:       "/factory-reset",
		Handler: func(_ context.Context, req Request, rt Runtime) error {
			if rt == nil || !rt.IsAdmin() {
				return req.Reply("Permission denied.")
			}
			if err := rt.FactoryReset(); err != nil {
				return req.Reply("Factory reset failed: " + err.Error())
			}
			return req.Reply("Factory reset done. Soul, role, memories, and history wiped.")
		},
	}
}

func whoamiCommand() Definition {
	return Definition{
		Name:        "whoami",
		Description: "Show your current soul, role, and LLM.",
		Usage:       "/whoami",
		Handler: func(_ context.Context, req Request, rt Runtime) error {
			if rt == nil {
				return req.Reply("Info unavailable.")
			}
			info := rt.Info()
			return req.Reply(fmt.Sprintf("Soul: %s\nRole: %s\nLLM: %s/%s",
				info.Soul, info.Role, info.Provider, info.LLMModel))
		},
	}
}
