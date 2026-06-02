package main

import (
	"github.com/joho/godotenv"
	"github.com/sho0pi/god/cmd"
)

func main() {
	_ = godotenv.Load()
	cmd.Execute()
}
