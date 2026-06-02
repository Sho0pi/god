package config

import (
	"bytes"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const DefaultPath = "god.yaml"

type Config struct {
	LLM        LLMConfig             `mapstructure:"llm"`
	Database   DatabaseConfig        `mapstructure:"database"`
	Memory     MemoryConfig          `mapstructure:"memory"`
	Connectors ConnectorsConfig      `mapstructure:"connectors"`
	Tools      ToolsConfig           `mapstructure:"tools"`
	Souls      map[string]SoulConfig `mapstructure:"souls"`
	Roles      map[string]RoleConfig `mapstructure:"roles"`
	Admin      []string              `mapstructure:"admin"`
}

type RoleConfig struct {
	LLM   LLMProviderConfig `mapstructure:"llm"`
	Tools []string          `mapstructure:"tools"`
}

type LLMProviderConfig struct {
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
}

type SoulConfig struct {
	Prompt string `mapstructure:"prompt"`
	Model  string `mapstructure:"model"`
}

type DatabaseConfig struct {
	URL string `mapstructure:"url"`
}

type MemoryConfig struct {
	TopK              int           `mapstructure:"top_k"`
	MaxTurns          int           `mapstructure:"max_turns"`
	InactivityTimeout time.Duration `mapstructure:"inactivity_timeout"`
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
	DefaultSoul  string             `mapstructure:"default_soul"`
	DefaultRole  string             `mapstructure:"default_role"`
}

type CLIConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	DefaultSoul string `mapstructure:"default_soul"`
	DefaultRole string `mapstructure:"default_role"`
}

// GroupTriggerConfig controls when the bot responds in group chats.
type GroupTriggerConfig struct {
	MentionOnly bool     `mapstructure:"mention_only"`
	Prefixes    []string `mapstructure:"prefixes"`
}

type ToolsConfig struct {
	Places   ToolConfig     `mapstructure:"places"`
	Exec     ExecToolConfig `mapstructure:"exec"`
	Config   ToolConfig     `mapstructure:"config"`   // lets god edit god.yaml (admin only)
	Approval []string       `mapstructure:"approval"` // tool names that require admin /approve before running
}

type ToolConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// ExecToolConfig configures the sandboxed shell-exec tool. Disabled by default:
// it is an LLM-callable shell and must only be granted to trusted roles.
type ExecToolConfig struct {
	Enabled   bool          `mapstructure:"enabled"`
	Image     string        `mapstructure:"image"`      // docker image the command runs in
	Timeout   time.Duration `mapstructure:"timeout"`    // wall-clock limit per command
	Memory    string        `mapstructure:"memory"`     // docker --memory (e.g. "256m")
	CPUs      string        `mapstructure:"cpus"`       // docker --cpus (e.g. "0.5")
	PidsLimit int           `mapstructure:"pids_limit"` // docker --pids-limit
	Network   bool          `mapstructure:"network"`    // false → --network=none
}

// Loader holds viper + a live, mutex-protected copy of the parsed config.
// Use Supplier() to get a func that always returns the latest config.
type Loader struct {
	v   *viper.Viper
	mu  sync.RWMutex
	cfg *Config

	// Cfg is kept for backward compat (initial value only — not updated on reload).
	Cfg *Config
}

// Load reads the config file and returns a Loader.
func Load(path string) (*Loader, error) {
	v := viper.New()

	v.SetDefault("connectors.whatsapp.enabled", true)
	v.SetDefault("connectors.whatsapp.store_path", "data/whatsapp")
	v.SetDefault("connectors.cli.enabled", true)
	v.SetDefault("tools.places.enabled", true)
	v.SetDefault("tools.exec.enabled", false)
	v.SetDefault("tools.exec.image", "alpine:3.20")
	v.SetDefault("tools.exec.timeout", "30s")
	v.SetDefault("tools.exec.memory", "256m")
	v.SetDefault("tools.exec.cpus", "0.5")
	v.SetDefault("tools.exec.pids_limit", 128)
	v.SetDefault("tools.exec.network", false)
	v.SetDefault("tools.config.enabled", false)
	v.SetDefault("memory.top_k", 5)
	v.SetDefault("memory.max_turns", 40)
	v.SetDefault("memory.inactivity_timeout", "30m")

	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	l := &Loader{v: v, cfg: &cfg, Cfg: &cfg}
	return l, nil
}

// Parse validates raw YAML config content, returning the parsed Config or an
// error. Used to validate edits before writing them to disk.
func Parse(content []byte) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

// Get returns the current config (thread-safe).
func (l *Loader) Get() *Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg
}

// Supplier returns a func that always returns the latest config.
// Pass this to components that need live updates without explicit callbacks.
func (l *Loader) Supplier() func() *Config {
	return l.Get
}

// Watch starts watching the config file. On each change it updates the internal
// config (available via Get/Supplier) and calls the optional onChange callback.
// Pass nil for onChange if you only need Supplier-based access.
func (l *Loader) Watch(onChange func(*Config)) {
	l.v.WatchConfig()
	l.v.OnConfigChange(func(e fsnotify.Event) {
		var cfg Config
		if err := l.v.Unmarshal(&cfg); err != nil {
			log.Printf("config: reload error: %v", err)
			return
		}
		l.mu.Lock()
		l.cfg = &cfg
		l.mu.Unlock()

		log.Printf("config: reloaded %s", e.Name)

		if onChange != nil {
			onChange(&cfg)
		}
	})
}
