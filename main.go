package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/sho0pi/god/agent"
	"github.com/sho0pi/god/connector/whatsapp"
	"github.com/sho0pi/god/llm/gemini"
)

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.0-flash"
	}

	storePath := os.Getenv("WHATSAPP_STORE_PATH")
	if storePath == "" {
		storePath = "data/whatsapp"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	llmClient, err := gemini.New(ctx, apiKey, model)
	if err != nil {
		log.Fatalf("gemini init: %v", err)
	}
	defer llmClient.Close()

	waConnector := whatsapp.New(storePath)
	a := agent.New(waConnector, llmClient)

	if err := a.Run(ctx); err != nil {
		log.Printf("agent stopped: %v", err)
	}
}
