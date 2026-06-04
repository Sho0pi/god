package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Request carries an incoming slash command plus a reply callback.
type Request struct {
	Text      string
	ChatID    string
	UserID    string
	Connector string
	Reply     func(text string) error
}

// Runtime gives command handlers access to agent capabilities scoped to the
// current request. The agent supplies a concrete implementation per command.
// Methods backed by a store return ErrUnsupported when none is configured.
type Runtime interface {
	ClearHistory() error
	IsAdmin() bool
	FactoryReset() error // admin only: wipes soul + role + memories + history
	Info() UserInfo
	// Allow-list management (admin only), scoped to the request's connector.
	AllowAdd(number string) error
	AllowRemove(number string) error
	AllowList() ([]string, error)
	// ResolveApproval approves (true) or denies (false) a parked tool call by id.
	// It sends its own replies and continues the agent's tool loop.
	ResolveApproval(approve bool, id string)
	// GenerateLinkCode mints a one-time code so another chat can link to this
	// account (see RedeemLinkCode). Returns ErrUnsupported with no store.
	GenerateLinkCode() (string, error)
	// RedeemLinkCode links this identity to the account that generated code,
	// returning a label for the now-shared account.
	RedeemLinkCode(code string) (string, error)
	// Unlink detaches this identity from its linked account.
	Unlink() error
	// LinkStatus reports whether this identity is linked, with a short detail.
	LinkStatus() (linked bool, detail string)
	// ListReminders returns this user's reminders as preformatted lines.
	ListReminders() ([]string, error)
	// CancelReminder cancels a reminder by id (owner-scoped); ok=false if not found.
	CancelReminder(id int64) (ok bool, err error)
}

// ErrUnsupported is returned by Runtime methods whose capability is unavailable
// in the current configuration (e.g. allow-list ops with no store).
var ErrUnsupported = errors.New("not available in this configuration")

// UserInfo carries the resolved identity for the current request.
type UserInfo struct {
	Connector string
	UserID    string
	Soul      string
	Role      string
	LLMModel  string
	Provider  string
}

// Definition describes a slash command.
type Definition struct {
	Name        string
	Description string
	Usage       string
	Handler     func(ctx context.Context, req Request, rt Runtime) error
}

// Registry maps slash command names to their definitions.
type Registry struct {
	defs  []Definition
	index map[string]int // lowercase name → index into defs
}

// NewRegistry builds a Registry from defs and auto-registers /help.
func NewRegistry(defs []Definition) *Registry {
	r := &Registry{index: make(map[string]int)}
	for _, d := range defs {
		r.add(d)
	}
	r.add(Definition{
		Name:        "help",
		Description: "List available commands",
		Usage:       "/help",
		Handler: func(_ context.Context, req Request, _ Runtime) error {
			var sb strings.Builder
			// Backtick the usage so it renders as monospace (and, on Telegram,
			// tap-to-copy) in chat.
			for _, d := range r.defs {
				fmt.Fprintf(&sb, "`%s` — %s\n", d.Usage, d.Description)
			}
			return req.Reply(strings.TrimSpace(sb.String()))
		},
	})
	return r
}

func (r *Registry) add(d Definition) {
	r.index[strings.ToLower(d.Name)] = len(r.defs)
	r.defs = append(r.defs, d)
}

// Lookup finds a command by name (without the leading slash), case-insensitive.
func (r *Registry) Lookup(name string) (Definition, bool) {
	i, ok := r.index[strings.ToLower(name)]
	if !ok {
		return Definition{}, false
	}
	return r.defs[i], true
}

// All returns a copy of all registered definitions.
func (r *Registry) All() []Definition {
	out := make([]Definition, len(r.defs))
	copy(out, r.defs)
	return out
}
