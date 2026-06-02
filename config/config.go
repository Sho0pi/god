package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

const DefaultPath = "god.yaml"

type Config struct {
	LLM        LLMConfig        `mapstructure:"llm"`
	Connectors ConnectorsConfig `mapstructure:"connectors"`
	Tools      ToolsConfig      `mapstructure:"tools"`
}

type LLMConfig struct {
	Model string `mapstructure:"model"`
}

type ConnectorsConfig struct {
	WhatsApp WhatsAppConfig `mapstructure:"whatsapp"`
	CLI      CLIConfig      `mapstructure:"cli"`
}

type WhatsAppConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	StorePath string   `mapstructure:"store_path"`
	Allow     []string `mapstructure:"allow"`
}

type CLIConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type ToolsConfig struct {
	Places ToolConfig `mapstructure:"places"`
}

type ToolConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetDefault("connectors.whatsapp.enabled", true)
	v.SetDefault("connectors.whatsapp.store_path", "data/whatsapp")
	v.SetDefault("connectors.cli.enabled", true)
	v.SetDefault("tools.places.enabled", true)

	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		// SetConfigFile returns a path error when the file is missing — that's fine.
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
