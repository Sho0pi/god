package config

import (
	"fmt"
	"log"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const DefaultPath = "god.yaml"

type Config struct {
	LLM        LLMConfig        `mapstructure:"llm"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Memory     MemoryConfig     `mapstructure:"memory"`
	Connectors ConnectorsConfig `mapstructure:"connectors"`
	Tools      ToolsConfig      `mapstructure:"tools"`
}

type DatabaseConfig struct {
	URL string `mapstructure:"url"`
}

type MemoryConfig struct {
	TopK int `mapstructure:"top_k"`
}

type LLMConfig struct {
	Model string `mapstructure:"model"`
}

type ConnectorsConfig struct {
	WhatsApp WhatsAppConfig `mapstructure:"whatsapp"`
	CLI      CLIConfig      `mapstructure:"cli"`
}

type WhatsAppConfig struct {
	Enabled      bool               `mapstructure:"enabled"`
	StorePath    string             `mapstructure:"store_path"`
	Allow        []string           `mapstructure:"allow"`
	GroupTrigger GroupTriggerConfig `mapstructure:"group_trigger"`
}

type CLIConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// GroupTriggerConfig controls when the bot responds in group chats.
// mention_only: only respond when @mentioned.
// prefixes: respond when message starts with one of these strings.
// neither set: respond to all group messages.
type GroupTriggerConfig struct {
	MentionOnly bool     `mapstructure:"mention_only"`
	Prefixes    []string `mapstructure:"prefixes"`
}

type ToolsConfig struct {
	Places ToolConfig `mapstructure:"places"`
}

type ToolConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// Loader holds the viper instance so callers can watch for config changes.
type Loader struct {
	v   *viper.Viper
	Cfg *Config
}

// Load reads the config file and returns a Loader.
func Load(path string) (*Loader, error) {
	v := viper.New()

	v.SetDefault("connectors.whatsapp.enabled", true)
	v.SetDefault("connectors.whatsapp.store_path", "data/whatsapp")
	v.SetDefault("connectors.cli.enabled", true)
	v.SetDefault("tools.places.enabled", true)
	v.SetDefault("memory.top_k", 5)

	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &Loader{v: v, Cfg: &cfg}, nil
}

// Watch starts watching the config file for changes and calls onChange with
// the updated config each time the file is written.
func (l *Loader) Watch(onChange func(*Config)) {
	l.v.WatchConfig()
	l.v.OnConfigChange(func(e fsnotify.Event) {
		var cfg Config
		if err := l.v.Unmarshal(&cfg); err != nil {
			log.Printf("config: reload error: %v", err)
			return
		}
		log.Printf("config: reloaded %s", e.Name)
		onChange(&cfg)
	})
}
