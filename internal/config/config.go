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
	Config     ToolConfig           `mapstructure:"config"`      // lets god edit god.yaml (admin only)
	WebExtract WebExtractToolConfig `mapstructure:"web_extract"` // fetch + read web pages
	FS         FSToolConfig         `mapstructure:"fs"`          // filesystem tools (read_file)
	Approval   []string             `mapstructure:"approval"`    // tool names that require admin /approve before running
}

// FSToolConfig configures the filesystem tools (read_file). Every path the model
// passes is untrusted, so access is contained to Root. Disabled by default.
type FSToolConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	Root         string `mapstructure:"root"`           // the ONLY dir tools may touch (empty → working dir)
	MaxReadBytes int64  `mapstructure:"max_read_bytes"` // per-read byte cap
}

// WebExtractToolConfig configures the web_extract tool, which fetches web pages
// and returns their content as markdown (optionally LLM-summarized). Every URL
// is untrusted, so BlockPrivate (SSRF guard) should stay true in production.
type WebExtractToolConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	MaxChars          int           `mapstructure:"max_chars"`           // truncate each page to this many runes
	Summarize         bool          `mapstructure:"summarize"`           // summarize large pages via the LLM
	SummarizeMinChars int           `mapstructure:"summarize_min_chars"` // pages shorter than this skip the LLM
	Timeout           time.Duration `mapstructure:"timeout"`             // per-request timeout
	BlockPrivate      bool          `mapstructure:"block_private"`       // SSRF guard: block non-public addresses
}

type ToolConfig struct {
	Enabled bool `mapstructure:"enabled"`
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
	v.SetDefault("tools.config.enabled", false)
	v.SetDefault("tools.web_extract.enabled", true)
	v.SetDefault("tools.web_extract.max_chars", 8000)
	v.SetDefault("tools.web_extract.summarize", true)
	v.SetDefault("tools.web_extract.summarize_min_chars", 5000)
	v.SetDefault("tools.web_extract.timeout", "15s")
	v.SetDefault("tools.web_extract.block_private", true)
	v.SetDefault("tools.fs.enabled", false)
	v.SetDefault("tools.fs.max_read_bytes", 10*1024*1024)
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
