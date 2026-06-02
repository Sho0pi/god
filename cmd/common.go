package cmd

import (
	"context"
	"log"
	"os"

	"github.com/sho0pi/god/agent"
	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/llm/gemini"
)

func runAgent(ctx context.Context, c connector.Connector) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-3.1-flash-lite"
	}

	llmClient, err := gemini.New(ctx, apiKey, model)
	if err != nil {
		log.Fatalf("gemini init: %v", err)
	}
	defer llmClient.Close()

	if err := agent.New(c, llmClient).Run(ctx); err != nil {
		log.Printf("agent stopped: %v", err)
	}
}
