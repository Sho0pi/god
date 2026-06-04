package cmd

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/sho0pi/god/internal/agent"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/embed"
	embedgemini "github.com/sho0pi/god/internal/embed/gemini"
	"github.com/sho0pi/god/internal/llm"
	llmanthropic "github.com/sho0pi/god/internal/llm/anthropic"
	llmgemini "github.com/sho0pi/god/internal/llm/gemini"
	llmopenai "github.com/sho0pi/god/internal/llm/openai"
	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/store/postgres"
	toolpkg "github.com/sho0pi/god/internal/tools"
	"github.com/sho0pi/god/internal/tools/configtool"
	"github.com/sho0pi/god/internal/tools/fs"
	"github.com/sho0pi/god/internal/tools/memory"
	"github.com/sho0pi/god/internal/tools/remind"
	toolsoul "github.com/sho0pi/god/internal/tools/soul"
	"github.com/sho0pi/god/internal/tools/webextract"
	"github.com/sho0pi/god/internal/tools/websearch"
)

// llmSummarizer adapts an llm.LLM to webextract.Summarizer so the web_extract
// tool can shrink large pages with the model. Kept here (not in webextract) so
// the tool package has no dependency on the llm package.
type llmSummarizer struct{ l llm.LLM }

func (s llmSummarizer) Summarize(ctx context.Context, content, instruction string) (string, error) {
	resp, err := s.l.ChatWithSystem(ctx, instruction, []llm.Message{{Role: "user", Text: content}}, nil)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (a *app) buildStore(ctx context.Context) store.Store {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = a.cfg.Database.URL
	}
	if url == "" {
		slog.Info("store: DATABASE_URL not set — memory disabled")
		return nil
	}
	s, err := postgres.New(ctx, url)
	if err != nil {
		slog.Error("store", "err", err)
		os.Exit(1)
	}
	slog.Info("store: connected to postgres")
	// Wrap so soul/role/memory ops resolve to a user's canonical (linked)
	// identity — see internal/store/canonical.go and the /link command.
	return store.Canonical(s)
}

func buildEmbedder(ctx context.Context, apiKey string) embed.Embedder {
	e, err := embedgemini.New(ctx, apiKey)
	if err != nil {
		slog.Error("embedder init failed", "err", err)
		return nil
	}
	slog.Info("embedder: text-embedding-004 ready")
	return e
}

// llmFactory builds the provider→client factory shared by the default LLM and
// the pool. API keys come from the environment (loaded from ~/.god/.env in main;
// the `god model` wizard writes them there).
func (a *app) llmFactory() llm.Factory {
	return func(ctx context.Context, pcfg llm.ProviderConfig) (llm.LLM, error) {
		switch pcfg.Provider {
		case "gemini", "google", "":
			return llmgemini.New(ctx, os.Getenv("GEMINI_API_KEY"), pcfg.Model)
		case "openai":
			return llmopenai.New(ctx, os.Getenv("OPENAI_API_KEY"), pcfg.Model)
		case "anthropic", "claude":
			return llmanthropic.New(ctx, os.Getenv("ANTHROPIC_API_KEY"), pcfg.Model)
		default:
			return nil, llm.ErrUnsupportedProvider(pcfg.Provider)
		}
	}
}

func (a *app) buildLLMPool(ctx context.Context, def llm.LLM) *llm.Pool {
	pool := llm.NewPool(a.llmFactory(), def)
	// Pre-warm role LLMs at startup. Pool.Get falls back to the default client on
	// failure (never nil), so detect a failed init by the fallback identity.
	for name, role := range a.cfg.Roles {
		if role.LLM.Provider == "" || role.LLM.Model == "" {
			continue
		}
		if l := pool.Get(ctx, llm.ProviderConfig{Provider: role.LLM.Provider, Model: role.LLM.Model}); l == def {
			slog.Warn("llm pool: role LLM init failed — using default (check provider API key)", "role", name, "provider", role.LLM.Provider, "model", role.LLM.Model)
		} else {
			slog.Info("llm pool", "role", name, "provider", role.LLM.Provider, "model", role.LLM.Model)
		}
	}
	return pool
}

// defaultModelFor returns a built-in fallback model for a provider when none is
// configured. Only Gemini has one; other providers must set llm.model.
func defaultModelFor(provider string) string {
	switch provider {
	case "gemini", "google", "":
		return "gemini-3.1-flash-lite"
	default:
		return ""
	}
}

// buildRegistry registers the provider-neutral tools (internal/tools). For now
// only the web tools are wired; the legacy internal/tool/* tools are kept in the
// tree but intentionally unregistered until they are migrated to the new Tool
// interface. def is the default LLM, used to summarize large web_extract pages.
func (a *app) buildRegistry(def llm.LLM, s store.Store, e embed.Embedder, sched *agent.Scheduler) *toolpkg.Registry {
	r := toolpkg.NewRegistry()

	r.Register(websearch.New(nil))
	slog.Info("tool: web_search enabled (requires ddg-search CLI on PATH)")

	if sched != nil {
		r.Register(remind.New(sched))
		slog.Info("tool: remind enabled (scheduled reminders)")
	}

	if s != nil && e != nil {
		r.Register(memory.NewRememberTool(e, s))
		slog.Info("tool: remember enabled (long-term memory)")
	}

	if s != nil {
		knownSouls := make([]string, 0, len(a.cfg.Souls))
		for name := range a.cfg.Souls {
			if name != "god" {
				knownSouls = append(knownSouls, name)
			}
		}
		r.Register(toolsoul.NewSetSoulTool(s, knownSouls))
		slog.Info("tool: set_soul enabled")
	}

	if a.cfg.Tools.Config.Enabled {
		// a.cfgFile is always set by PersistentPreRunE to the resolved config
		// path (~/.god/god.yaml by default); the config tool edits that file.
		r.Register(configtool.New(a.cfgFile))
		slog.Info("tool: config enabled (god edits god.yaml — grant to admin role only; approval recommended)")
	}

	if a.cfg.Tools.WebExtract.Enabled {
		supply := a.loader.Supplier()
		cfgFn := func() webextract.Config {
			c := supply().Tools.WebExtract
			return webextract.Config{
				MaxChars:          c.MaxChars,
				Summarize:         c.Summarize,
				SummarizeMinChars: c.SummarizeMinChars,
				Timeout:           c.Timeout,
				BlockPrivate:      c.BlockPrivate,
			}
		}
		var summarizer webextract.Summarizer
		if def != nil {
			summarizer = llmSummarizer{l: def}
		}
		r.Register(webextract.New(cfgFn, summarizer))
		slog.Info("tool: web_extract enabled (SSRF guard on; large pages summarized via default LLM)")
	}

	if a.cfg.Tools.FS.Enabled {
		ws, err := fs.New(fs.Config{
			Root:         a.cfg.Tools.FS.Root,
			MaxReadBytes: a.cfg.Tools.FS.MaxReadBytes,
		})
		if err != nil {
			slog.Error("tool: read_file disabled", "err", err)
		} else {
			r.Register(fs.NewReadFileTool(ws))
			r.Register(fs.NewListDirTool(ws))
			r.Register(fs.NewGlobTool(ws, nil))
			r.Register(fs.NewGrepTool(ws, nil))
			r.Register(fs.NewWriteFileTool(ws))
			r.Register(fs.NewEditFileTool(ws))
			slog.Info("tool: read_file, list_dir, glob, grep, write_file, edit_file enabled (WRITES are ungated)", "root", ws.Root())
		}
	}

	return r
}

func (a *app) runAgent(ctx context.Context, c connector.Connector) {
	cfg := a.cfg

	// The default LLM (used when a role/soul has no llm of its own) can be any
	// provider, set via `llm.provider` / `llm.model` (run `god model`).
	provider := cfg.LLM.Provider
	model := cfg.LLM.Model
	if model == "" {
		model = defaultModelFor(provider)
	}
	if model == "" {
		slog.Error("no default model configured — run `god model` or set llm.model", "provider", provider)
		os.Exit(1)
	}

	defaultLLM, err := a.llmFactory()(ctx, llm.ProviderConfig{Provider: provider, Model: model})
	if err != nil {
		slog.Error("default LLM init failed — check the provider's API key (run `god model`)", "provider", provider, "err", err)
		os.Exit(1)
	}
	if cl, ok := defaultLLM.(interface{ Close() error }); ok {
		defer func() { _ = cl.Close() }()
	}

	s := a.buildStore(ctx)
	if s != nil {
		defer func() { _ = s.Close() }()
		// If the connector supports a runtime allow source, feed it store-backed
		// allow-list entries (managed via the /allow admin command).
		if as, ok := c.(interface{ SetAllowSource(func() []string) }); ok {
			as.SetAllowSource(func() []string {
				qctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				nums, err := s.ListAllow(qctx, "whatsapp")
				if err != nil {
					slog.Warn("allow source", "err", err)
					return nil
				}
				return nums
			})
		}
	}

	// Embeddings (long-term memory) are Gemini-only; build the embedder only when
	// a Gemini key is present, regardless of the default chat provider.
	var e embed.Embedder
	if s != nil {
		if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
			e = buildEmbedder(ctx, geminiKey)
		} else {
			slog.Warn("memory: GEMINI_API_KEY not set — long-term memory disabled (embeddings need Gemini)")
		}
	}

	pool := a.buildLLMPool(ctx, defaultLLM)
	supply := a.loader.Supplier()
	a.loader.Watch(nil) // keeps loader's internal cfg updated; supplier reads it

	// Scheduler (reminders) needs a store. Built before the registry so the
	// remind tool can capture it; wired with a runner after the agent exists.
	var sched *agent.Scheduler
	if s != nil {
		var err error
		if sched, err = agent.NewScheduler(s); err != nil {
			slog.Error("scheduler init", "err", err)
			sched = nil
		}
	}

	ag := agent.New(c, defaultLLM, a.buildRegistry(defaultLLM, s, e, sched), e, s, agent.Options{
		MaxTurns:          cfg.Memory.MaxTurns,
		InactivityTimeout: cfg.Memory.InactivityTimeout,
		ConfigFn:          supply,
		LLMPool:           pool,
		Scheduler:         sched,
	})

	if sched != nil {
		sched.SetRunner(ag.RunInstruction)
		if err := sched.Start(ctx); err != nil {
			slog.Error("scheduler start", "err", err)
		}
	}

	if err := ag.Run(ctx); err != nil {
		slog.Error("agent stopped", "err", err)
	}
}
