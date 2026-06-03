package cmd

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/sho0pi/god/internal/agent"
	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/embed"
	embedgemini "github.com/sho0pi/god/internal/embed/gemini"
	"github.com/sho0pi/god/internal/llm"
	llmgemini "github.com/sho0pi/god/internal/llm/gemini"
	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/store/postgres"
	"github.com/sho0pi/god/internal/tool"
	"github.com/sho0pi/god/internal/tool/calculator"
	"github.com/sho0pi/god/internal/tool/cfgtool"
	toolexec "github.com/sho0pi/god/internal/tool/exec"
	"github.com/sho0pi/god/internal/tool/memory"
	toolplaces "github.com/sho0pi/god/internal/tool/places"
	toolsoul "github.com/sho0pi/god/internal/tool/soul"
	"github.com/sho0pi/god/internal/tool/websearch"
)

func (a *app) buildStore(ctx context.Context) store.Store {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = a.cfg.Database.URL
	}
	if url == "" {
		log.Println("store: DATABASE_URL not set — memory disabled")
		return nil
	}
	s, err := postgres.New(ctx, url)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	log.Println("store: connected to postgres")
	return s
}

func buildEmbedder(ctx context.Context, apiKey string) embed.Embedder {
	e, err := embedgemini.New(ctx, apiKey)
	if err != nil {
		log.Printf("embedder init failed: %v", err)
		return nil
	}
	log.Println("embedder: text-embedding-004 ready")
	return e
}

func (a *app) buildLLMPool(ctx context.Context, geminiKey string, def llm.LLM) *llm.Pool {
	factory := func(ctx context.Context, pcfg llm.ProviderConfig) (llm.LLM, error) {
		switch pcfg.Provider {
		case "gemini", "google":
			key := os.Getenv("GEMINI_API_KEY")
			if key == "" {
				key = geminiKey
			}
			return llmgemini.New(ctx, key, pcfg.Model)
		default:
			return nil, llm.ErrUnsupportedProvider(pcfg.Provider)
		}
	}
	pool := llm.NewPool(factory, def)
	// Pre-warm role LLMs at startup.
	for name, role := range a.cfg.Roles {
		if role.LLM.Provider == "" || role.LLM.Model == "" {
			continue
		}
		if l := pool.Get(ctx, llm.ProviderConfig{Provider: role.LLM.Provider, Model: role.LLM.Model}); l == nil {
			log.Printf("llm pool: failed to init role %q LLM", name)
		} else {
			log.Printf("llm pool: role %q → %s/%s", name, role.LLM.Provider, role.LLM.Model)
		}
	}
	return pool
}

func (a *app) buildRegistry(s store.Store, e embed.Embedder) *tool.Registry {
	cfg := a.cfg
	r := tool.NewRegistry()

	r.Register(calculator.New())
	log.Println("tool: calculator enabled")

	r.Register(websearch.New())
	log.Println("tool: web_search enabled")

	if s != nil {
		knownSouls := make([]string, 0, len(cfg.Souls))
		for name := range cfg.Souls {
			if name != "god" {
				knownSouls = append(knownSouls, name)
			}
		}
		r.Register(toolsoul.NewSetSoulTool(s, knownSouls))
		log.Println("tool: set_soul enabled")
	}

	if cfg.Tools.Places.Enabled {
		if key := os.Getenv("GOOGLE_PLACES_API_KEY"); key != "" {
			r.Register(toolplaces.NewSearchTool(key))
			log.Println("tool: search_places enabled")
		}
	}

	if cfg.Tools.Config.Enabled {
		path := a.cfgFile
		if path == "" {
			path = config.DefaultPath
		}
		r.Register(cfgtool.New(path))
		log.Println("tool: config enabled (god edits god.yaml — grant to admin role only)")
	}

	if cfg.Tools.Exec.Enabled {
		t, err := toolexec.New(toolexec.Config{
			Image:     cfg.Tools.Exec.Image,
			Timeout:   cfg.Tools.Exec.Timeout,
			Memory:    cfg.Tools.Exec.Memory,
			CPUs:      cfg.Tools.Exec.CPUs,
			PidsLimit: cfg.Tools.Exec.PidsLimit,
			Network:   cfg.Tools.Exec.Network,
		})
		if err != nil {
			log.Printf("tool: exec disabled: %v", err)
		} else {
			r.Register(t)
			log.Println("tool: exec enabled (sandboxed docker — grant to trusted roles only)")
		}
	}

	if s != nil && e != nil {
		r.Register(memory.NewRememberTool(e, s))
		log.Println("tool: remember enabled")
	}

	return r
}

func (a *app) runAgent(ctx context.Context, c connector.Connector) {
	cfg := a.cfg
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	model := cfg.LLM.Model
	if model == "" {
		model = os.Getenv("GEMINI_MODEL")
	}
	if model == "" {
		model = "gemini-3.1-flash-lite"
	}

	defaultLLM, err := llmgemini.New(ctx, geminiKey, model)
	if err != nil {
		log.Fatalf("gemini init: %v", err)
	}
	defer defaultLLM.Close()

	s := a.buildStore(ctx)
	if s != nil {
		defer s.Close()
		// If the connector supports a runtime allow source, feed it store-backed
		// allow-list entries (managed via the /allow admin command).
		if as, ok := c.(interface{ SetAllowSource(func() []string) }); ok {
			as.SetAllowSource(func() []string {
				qctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				nums, err := s.ListAllow(qctx, "whatsapp")
				if err != nil {
					log.Printf("allow source: %v", err)
					return nil
				}
				return nums
			})
		}
	}

	var e embed.Embedder
	if s != nil {
		e = buildEmbedder(ctx, geminiKey)
	}

	topK := cfg.Memory.TopK
	if topK == 0 {
		topK = 5
	}

	pool := a.buildLLMPool(ctx, geminiKey, defaultLLM)
	supply := a.loader.Supplier()
	a.loader.Watch(nil) // keeps loader's internal cfg updated; supplier reads it

	ag := agent.New(c, defaultLLM, a.buildRegistry(s, e), e, s, agent.Options{
		MaxTurns:          cfg.Memory.MaxTurns,
		InactivityTimeout: cfg.Memory.InactivityTimeout,
		ConfigFn:          supply,
		LLMPool:           pool,
	})
	if err := ag.Run(ctx); err != nil {
		log.Printf("agent stopped: %v", err)
	}
}
