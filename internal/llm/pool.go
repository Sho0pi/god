package llm

import (
	"context"
	"fmt"
	"sync"
)

// ProviderConfig selects an LLM provider and model.
type ProviderConfig struct {
	Provider string // "gemini", "openai", "anthropic"
	Model    string
}

// Factory creates an LLM client for the given provider config.
type Factory func(ctx context.Context, cfg ProviderConfig) (LLM, error)

// Pool caches LLM clients by provider+model, creating them lazily via a Factory.
type Pool struct {
	mu      sync.RWMutex
	clients map[string]LLM
	factory Factory
	def     LLM // fallback when role has no LLM config or factory fails
}

// NewPool creates a Pool with the given factory function and default LLM.
func NewPool(factory Factory, def LLM) *Pool {
	return &Pool{
		clients: make(map[string]LLM),
		factory: factory,
		def:     def,
	}
}

// Get returns a cached or newly created LLM for the given config.
// Falls back to the default LLM if provider/model are empty or creation fails.
func (p *Pool) Get(ctx context.Context, cfg ProviderConfig) LLM {
	if cfg.Provider == "" || cfg.Model == "" {
		return p.def
	}

	key := cfg.Provider + ":" + cfg.Model

	p.mu.RLock()
	if l, ok := p.clients[key]; ok {
		p.mu.RUnlock()
		return l
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	// Re-check under the write lock: another goroutine may have created it
	// between the RUnlock above and acquiring this lock.
	if l, ok := p.clients[key]; ok {
		return l
	}

	l, err := p.factory(ctx, cfg)
	if err != nil {
		return p.def
	}
	p.clients[key] = l
	return l
}

// ErrUnsupportedProvider is returned by a Factory for unknown providers.
func ErrUnsupportedProvider(provider string) error {
	return fmt.Errorf("unsupported LLM provider %q", provider)
}
