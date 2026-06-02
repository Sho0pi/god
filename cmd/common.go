package cmd

import (
	"context"
	"log"
	"os"

	"github.com/sho0pi/god/agent"
	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/embed"
	embedgemini "github.com/sho0pi/god/embed/gemini"
	"github.com/sho0pi/god/llm/gemini"
	"github.com/sho0pi/god/store"
	"github.com/sho0pi/god/store/postgres"
	"github.com/sho0pi/god/tool"
	"github.com/sho0pi/god/tool/memory"
	toolplaces "github.com/sho0pi/god/tool/places"
)

func buildStore(ctx context.Context) store.Store {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = cfg.Database.URL
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
		return nil // explicit nil interface — safe for agent nil-check
	}
	log.Println("embedder: text-embedding-004 ready")
	return e
}

func buildRegistry(s store.Store, e embed.Embedder) *tool.Registry {
	r := tool.NewRegistry()

	if cfg.Tools.Places.Enabled {
		if key := os.Getenv("GOOGLE_PLACES_API_KEY"); key != "" {
			r.Register(toolplaces.NewSearchTool(key))
			log.Println("tool: search_places enabled")
		}
	}

	if s != nil && e != nil {
		r.Register(memory.NewRememberTool(e, s))
		log.Println("tool: remember enabled")
	}

	return r
}

func runAgent(ctx context.Context, c connector.Connector) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	model := cfg.LLM.Model
	if model == "" {
		model = os.Getenv("GEMINI_MODEL")
	}
	if model == "" {
		model = "gemini-3.1-flash-lite"
	}

	llmClient, err := gemini.New(ctx, apiKey, model)
	if err != nil {
		log.Fatalf("gemini init: %v", err)
	}
	defer llmClient.Close()

	s := buildStore(ctx)
	if s != nil {
		defer s.Close()
	}

	var e embed.Embedder
	if s != nil {
		e = buildEmbedder(ctx, apiKey)
	}

	topK := cfg.Memory.TopK
	if topK == 0 {
		topK = 5
	}

	a := agent.New(c, llmClient, buildRegistry(s, e), e, s, topK)
	if err := a.Run(ctx); err != nil {
		log.Printf("agent stopped: %v", err)
	}
}
