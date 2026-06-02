// Package cfgtool provides an LLM-callable tool that lets god read and edit its
// own YAML config file (god.yaml) — and only that file. Edits are validated
// before being written, the previous version is backed up to <path>.bak, and
// the running process hot-reloads the new config via viper. This is the
// narrow, contained form of "god manages its own configuration": it cannot
// touch source code or any other file, only the one config path it was given.
//
// SECURITY: this is admin-only (gate via role tools). Because it edits config,
// a prompt-injection inside an admin session could try to rewrite sensitive
// fields (e.g. the admin list or allow list). The .bak backup is the safety
// net; review changes if you also expose untrusted-input tools to admins.
package cfgtool

import (
	"context"
	"fmt"
	"os"

	"github.com/sho0pi/god/config"
	"github.com/sho0pi/god/tool"
)

// Tool reads and writes a single YAML config file.
type Tool struct {
	path string
}

func New(path string) *Tool { return &Tool{path: path} }

func (t *Tool) Name() string { return "config" }

func (t *Tool) Description() string {
	return "Read or update god's own YAML configuration file (" + t.path + "). " +
		"action='get' returns the current config text. action='set' replaces the " +
		"entire file with new YAML (validated before saving; the old version is " +
		"backed up). Saved changes hot-reload immediately — no restart. Use this to " +
		"add allowed WhatsApp numbers, change souls/roles/admins, or tweak settings. " +
		"For 'set' you must provide the COMPLETE new file content, not a fragment."
}

func (t *Tool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"action": {
				Type:        "string",
				Description: "'get' to read the current config, 'set' to overwrite it.",
				Enum:        []string{"get", "set"},
			},
			"content": {
				Type:        "string",
				Description: "Required for 'set': the complete new YAML config file content.",
			},
		},
		Required: []string{"action"},
	}
}

func (t *Tool) Execute(_ context.Context, args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	switch action {
	case "get":
		b, err := os.ReadFile(t.path)
		if err != nil {
			return "", fmt.Errorf("read config: %w", err)
		}
		return string(b), nil

	case "set":
		content, _ := args["content"].(string)
		if content == "" {
			return "", fmt.Errorf("content is required for action 'set'")
		}
		// Validate before touching disk — reject anything that won't parse.
		if _, err := config.Parse([]byte(content)); err != nil {
			return "", fmt.Errorf("invalid config, not saved: %w", err)
		}
		// Back up the current file so a bad edit can be reverted.
		if old, err := os.ReadFile(t.path); err == nil {
			if err := os.WriteFile(t.path+".bak", old, 0o600); err != nil {
				return "", fmt.Errorf("write backup: %w", err)
			}
		}
		if err := os.WriteFile(t.path, []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("write config: %w", err)
		}
		return "Config updated and validated. Previous version saved to " + t.path + ".bak. Changes hot-reload now.", nil

	default:
		return "", fmt.Errorf("unknown action %q (use 'get' or 'set')", action)
	}
}
