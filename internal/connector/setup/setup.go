// Package setup provides the interactive setup flows ("wizards") for connectors,
// driving the `god connector` command. Each connector contributes one Wizard via
// Register (from an init() in this package), so the command and the connector
// list stay decoupled — adding a connector means adding one Wizard, nothing in
// the command changes.
package setup

import (
	"context"
	"sort"
	"sync"

	"github.com/sho0pi/god/internal/config"
)

// Wizard is one connector's interactive setup flow.
type Wizard interface {
	// Key is the connector's stable id and subcommand name, e.g. "telegram".
	Key() string
	// Title is the human-facing label, e.g. "Telegram".
	Title() string
	// Enabled reports the connector's current enabled flag from cfg.
	Enabled(cfg *config.Config) bool
	// SessionStatus reports whether the connector is already configured/paired
	// and a short human-readable detail (e.g. "paired", "no token").
	SessionStatus(cfg *config.Config) (exists bool, detail string)
	// Setup runs the interactive flow and returns the dotted-key config edits to
	// persist (see config.SetValues). reset requests a fresh session (re-pair /
	// re-enter credentials) even if one already exists.
	Setup(ctx context.Context, cfg *config.Config, reset bool) (edits map[string]any, err error)
}

var (
	mu      sync.RWMutex
	wizards = map[string]Wizard{}
	ordered []string // registration order preserved for the menu
)

// Register adds a wizard to the registry. Panics on a duplicate key, since that
// is a programming error (two connectors claiming the same name).
func Register(w Wizard) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := wizards[w.Key()]; dup {
		panic("setup: duplicate wizard key " + w.Key())
	}
	wizards[w.Key()] = w
	ordered = append(ordered, w.Key())
}

// All returns the registered wizards in registration order.
func All() []Wizard {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Wizard, 0, len(ordered))
	for _, k := range ordered {
		out = append(out, wizards[k])
	}
	return out
}

// Keys returns the registered wizard keys, sorted (for stable help/errors).
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]string{}, ordered...)
	sort.Strings(out)
	return out
}

// Lookup returns the wizard for key, if registered.
func Lookup(key string) (Wizard, bool) {
	mu.RLock()
	defer mu.RUnlock()
	w, ok := wizards[key]
	return w, ok
}
