// Package setup provides the interactive setup flows for LLM providers, driving
// the `god model` command. Each provider contributes one descriptor via Register
// (from an init() here), so the command stays decoupled from the provider list —
// adding a provider is one descriptor, nothing in the command changes.
package setup

import (
	"context"
	"sort"
	"sync"
)

// Provider describes one LLM backend for the setup wizard. The command runs a
// uniform flow (prompt key → validate → save to ~/.god/.env → optionally set as
// default); only these per-provider details differ.
type Provider interface {
	// Key is the provider's id and subcommand name, e.g. "gemini".
	Key() string
	// Title is the human-facing label, e.g. "Gemini".
	Title() string
	// EnvVar is the environment variable holding the API key, e.g.
	// "GEMINI_API_KEY". Used both to store the key and to report status.
	EnvVar() string
	// Models lists curated model ids for the picker (first is the suggested default).
	Models() []string
	// ValidateKey checks an API key with a live request, returning nil if valid.
	ValidateKey(ctx context.Context, key string) error
}

var (
	mu        sync.RWMutex
	providers = map[string]Provider{}
	ordered   []string // registration order preserved for the menu
)

// Register adds a provider. Panics on a duplicate key (a programming error).
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := providers[p.Key()]; dup {
		panic("llm/setup: duplicate provider key " + p.Key())
	}
	providers[p.Key()] = p
	ordered = append(ordered, p.Key())
}

// All returns the registered providers in registration order.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, 0, len(ordered))
	for _, k := range ordered {
		out = append(out, providers[k])
	}
	return out
}

// Keys returns the registered provider keys, sorted (for stable help/errors).
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]string{}, ordered...)
	sort.Strings(out)
	return out
}

// Lookup returns the provider for key, if registered.
func Lookup(key string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[key]
	return p, ok
}
