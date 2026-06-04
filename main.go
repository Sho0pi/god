package main

import (
	"github.com/joho/godotenv"

	"github.com/sho0pi/god/internal/cmd"
	"github.com/sho0pi/god/internal/godhome"
	"github.com/sho0pi/god/internal/logging"
)

func main() {
	loadEnv()
	logging.Setup()
	cmd.Execute()
}

// loadEnv populates the process environment from ~/.god/.env, falling back to a
// .env in the working directory for local development. godotenv never overrides
// variables already set in the real environment, and earlier files win, so the
// home .env takes precedence over the working-dir one.
func loadEnv() {
	if p, err := godhome.Path(".env"); err == nil {
		_ = godotenv.Load(p)
	}
	_ = godotenv.Load()
}
