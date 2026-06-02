package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/sho0pi/god/agent"
	cliconn "github.com/sho0pi/god/connector/cli"
	"github.com/sho0pi/god/connector/whatsapp"
	"github.com/sho0pi/god/llm/gemini"
)

func main() {
	_ = godotenv.Load()

	mode := flag.String("mode", "whatsapp", "connector mode: whatsapp or cli")
	flag.Parse()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-3.1-flash-lite"
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

	var a *agent.Agent
	switch *mode {
	case "cli":
		a = agent.New(cliconn.New(), llmClient)
	case "whatsapp":
		a = agent.New(whatsapp.New(storePath), llmClient)
	default:
		log.Fatalf("unknown mode %q — use 'cli' or 'whatsapp'", *mode)
	}

	if err := a.Run(ctx); err != nil {
		log.Printf("agent stopped: %v", err)
	}
}
