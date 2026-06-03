package fs

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sho0pi/god/internal/tools"
)

const maxListEntries = 500

// ListArgs are the list_dir arguments.
type ListArgs struct {
	Path string `json:"path"`
}

// NewListDirTool returns the list_dir tool bound to a workspace.
func NewListDirTool(ws *Workspace) tools.Tool {
	return tools.NewTypedTool(
		"list_dir",
		"List the entries of a directory (directories first, then files with sizes). "+
			"Path is relative to the workspace root; defaults to the root. Use this to "+
			"discover files before reading them with read_file.",
		tools.Object(map[string]*tools.Property{
			"path": {
				Type:        "string",
				Description: "Directory to list, relative to the workspace root (default: root).",
			},
		}),
		func(_ context.Context, args ListArgs) (tools.Result, error) {
			return ws.listDir(args)
		},
	)
}

func (w *Workspace) listDir(args ListArgs) (tools.Result, error) {
	p := args.Path
	if strings.TrimSpace(p) == "" {
		p = "."
	}
	resolved, err := w.resolve(p)
	if err != nil {
		return tools.Result{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return tools.Result{}, err
	}
	if !info.IsDir() {
		return tools.Result{}, fmt.Errorf("not a directory: %s", args.Path)
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return tools.Result{}, fmt.Errorf("list dir: %w", err)
	}

	// Directories first, then files; alphabetical within each group.
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})

	var sb strings.Builder
	shown := 0
	for _, e := range entries {
		if shown >= maxListEntries {
			break
		}
		if e.IsDir() {
			fmt.Fprintf(&sb, "[DIR]  %s/\n", e.Name())
		} else {
			size := int64(-1)
			if fi, err := e.Info(); err == nil {
				size = fi.Size()
			}
			fmt.Fprintf(&sb, "       %s (%d bytes)\n", e.Name(), size)
		}
		shown++
	}
	if len(entries) > maxListEntries {
		fmt.Fprintf(&sb, "[showing %d of %d entries]", maxListEntries, len(entries))
	} else {
		fmt.Fprintf(&sb, "[%d entries]", len(entries))
	}

	return tools.Result{
		Content: sb.String(),
		Data:    map[string]any{"path": p, "count": len(entries)},
	}, nil
}
