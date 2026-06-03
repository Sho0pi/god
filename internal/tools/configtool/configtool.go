// Package configtool provides an LLM-callable tool that lets god read and edit
// its own YAML config file (god.yaml) — and only that file. Edits are validated
// before being written, the previous version is backed up to <path>.bak, and
// the running process hot-reloads the new config via viper. This is the narrow,
// contained form of "god manages its own configuration": it cannot touch source
// code or any other file, only the one config path it was given.
//
// SECURITY: admin-only (gate via role tools) and best kept behind the approval
// gate (tools.approval). Because it edits config, a prompt-injection inside an
// admin session could try to rewrite sensitive fields (e.g. the admin or allow
// list). The .bak backup is the safety net.
package configtool

import (
	"context"
	"fmt"
	"os"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/tools"
)

// Args are the config tool arguments.
type Args struct {
	Action  string `json:"action"`
	Content string `json:"content"`
}

// New returns the config tool bound to a single YAML file path.
func New(path string) tools.Tool {
	return tools.NewTypedTool(
		"config",
		"Read or update god's own YAML configuration file ("+path+"). "+
			"action='get' returns the current config text. action='set' replaces the "+
			"entire file with new YAML (validated before saving; the old version is "+
			"backed up). Saved changes hot-reload immediately — no restart. Use this to "+
			"add allowed WhatsApp numbers, change souls/roles/admins, or tweak settings. "+
			"For 'set' you must provide the COMPLETE new file content, not a fragment.",
		tools.Object(map[string]*tools.Property{
			"action": {
				Type:        "string",
				Description: "'get' to read the current config, 'set' to overwrite it.",
				Enum:        []string{"get", "set"},
			},
			"content": {
				Type:        "string",
				Description: "Required for 'set': the complete new YAML config file content.",
			},
		}, "action"),
		func(ctx context.Context, args Args) (tools.Result, error) {
			return run(path, args)
		},
	)
}

func run(path string, args Args) (tools.Result, error) {
	switch args.Action {
	case "get":
		b, err := os.ReadFile(path)
		if err != nil {
			return tools.Result{}, fmt.Errorf("read config: %w", err)
		}
		return tools.Result{Content: string(b)}, nil

	case "set":
		if args.Content == "" {
			return tools.Result{}, fmt.Errorf("content is required for action 'set'")
		}
		// Validate before touching disk — reject anything that won't parse.
		if _, err := config.Parse([]byte(args.Content)); err != nil {
			return tools.Result{}, fmt.Errorf("invalid config, not saved: %w", err)
		}
		// Back up the current file so a bad edit can be reverted.
		if old, err := os.ReadFile(path); err == nil {
			if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
				return tools.Result{}, fmt.Errorf("write backup: %w", err)
			}
		}
		if err := os.WriteFile(path, []byte(args.Content), 0o600); err != nil {
			return tools.Result{}, fmt.Errorf("write config: %w", err)
		}
		return tools.Result{
			Content: "Config updated and validated. Previous version saved to " + path + ".bak. Changes hot-reload now.",
		}, nil

	default:
		return tools.Result{}, fmt.Errorf("unknown action %q (use 'get' or 'set')", args.Action)
	}
}
