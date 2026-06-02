package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "god.yaml"

type Config struct {
	LLM        LLMConfig        `yaml:"llm"`
	Connectors ConnectorsConfig `yaml:"connectors"`
	Tools      ToolsConfig      `yaml:"tools"`
}

type LLMConfig struct {
	Model string `yaml:"model"`
}

type ConnectorsConfig struct {
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`
	CLI      CLIConfig      `yaml:"cli"`
}

type WhatsAppConfig struct {
	Enabled   bool     `yaml:"enabled"`
	StorePath string   `yaml:"store_path"`
	Allow     []string `yaml:"allow"` // phone numbers; empty = allow all
}

type CLIConfig struct {
	Enabled bool `yaml:"enabled"`
}

type ToolsConfig struct {
	Places ToolConfig `yaml:"places"`
}

type ToolConfig struct {
	Enabled bool `yaml:"enabled"`
}

// Load reads config from path. If the file does not exist, defaults are returned.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Connectors: ConnectorsConfig{
			WhatsApp: WhatsAppConfig{Enabled: true},
			CLI:      CLIConfig{Enabled: true},
		},
		Tools: ToolsConfig{
			Places: ToolConfig{Enabled: true},
		},
	}
}
