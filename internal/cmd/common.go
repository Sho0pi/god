package cmd

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/sho0pi/god/internal/agent"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/embed"
	embedgemini "github.com/sho0pi/god/internal/embed/gemini"
	"github.com/sho0pi/god/internal/llm"
	llmgemini "github.com/sho0pi/god/internal/llm/gemini"
	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/store/postgres"
	toolpkg "github.com/sho0pi/god/internal/tools"
	"github.com/sho0pi/god/internal/tools/configtool"
	"github.com/sho0pi/god/internal/tools/fs"
	"github.com/sho0pi/god/internal/tools/memory"
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

// buildRegistry registers the provider-neutral tools (internal/tools). For now
// only the web tools are wired; the legacy internal/tool/* tools are kept in the
// tree but intentionally unregistered until they are migrated to the new Tool
// interface. def is the default LLM, used to summarize large web_extract pages.
func (a *app) buildRegistry(def llm.LLM, s store.Store, e embed.Embedder) *toolpkg.Registry {
	r := toolpkg.NewRegistry()

	r.Register(websearch.New(nil))
	log.Println("tool: web_search enabled (requires ddg-search CLI on PATH)")

	if s != nil && e != nil {
		r.Register(memory.NewRememberTool(e, s))
		log.Println("tool: remember enabled (long-term memory)")
	}

	if s != nil {
		knownSouls := make([]string, 0, len(a.cfg.Souls))
		for name := range a.cfg.Souls {
			if name != "god" {
				knownSouls = append(knownSouls, name)
			}
		}
		r.Register(toolsoul.NewSetSoulTool(s, knownSouls))
		log.Println("tool: set_soul enabled")
	}

	if a.cfg.Tools.Config.Enabled {
		// a.cfgFile is always set by PersistentPreRunE to the resolved config
		// path (~/.god/god.yaml by default); the config tool edits that file.
		r.Register(configtool.New(a.cfgFile))
		log.Println("tool: config enabled (god edits god.yaml — grant to admin role only; approval recommended)")
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
		log.Println("tool: web_extract enabled (SSRF guard on; large pages summarized via default LLM)")
	}

	if a.cfg.Tools.FS.Enabled {
		ws, err := fs.New(fs.Config{
			Root:         a.cfg.Tools.FS.Root,
			MaxReadBytes: a.cfg.Tools.FS.MaxReadBytes,
		})
		if err != nil {
			log.Printf("tool: read_file disabled: %v", err)
		} else {
			r.Register(fs.NewReadFileTool(ws))
			r.Register(fs.NewListDirTool(ws))
			r.Register(fs.NewGlobTool(ws, nil))
			r.Register(fs.NewGrepTool(ws, nil))
			r.Register(fs.NewWriteFileTool(ws))
			r.Register(fs.NewEditFileTool(ws))
			log.Printf("tool: read_file, list_dir, glob, grep, write_file, edit_file enabled (workspace root: %s — WRITES are ungated)", ws.Root())
		}
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
	defer func() { _ = defaultLLM.Close() }()

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

	pool := a.buildLLMPool(ctx, geminiKey, defaultLLM)
	supply := a.loader.Supplier()
	a.loader.Watch(nil) // keeps loader's internal cfg updated; supplier reads it

	ag := agent.New(c, defaultLLM, a.buildRegistry(defaultLLM, s, e), e, s, agent.Options{
		MaxTurns:          cfg.Memory.MaxTurns,
		InactivityTimeout: cfg.Memory.InactivityTimeout,
		ConfigFn:          supply,
		LLMPool:           pool,
	})
	if err := ag.Run(ctx); err != nil {
		log.Printf("agent stopped: %v", err)
	}
}
