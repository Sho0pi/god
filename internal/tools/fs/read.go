package fs

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/sho0pi/god/internal/tools"
)

// Args are the read_file arguments.
type Args struct {
	Path     string `json:"path"`
	Offset   int    `json:"offset"`   // 1-based start line (default 1); ignored for base64
	Limit    int    `json:"limit"`    // max lines to return (default all); ignored for base64
	Encoding string `json:"encoding"` // "utf8" (default) | "base64"
}

// NewReadFileTool returns the read_file tool bound to a workspace.
func NewReadFileTool(ws *Workspace) tools.Tool {
	return tools.NewTypedTool(
		"read_file",
		"Read a file's contents with line numbers. Paths are relative to the "+
			"workspace root; absolute paths must be inside it. Supports partial reads "+
			"via offset (1-based start line) and limit (max lines). Set "+
			`encoding="base64" to read a binary file as base64-encoded bytes.`,
		schema(),
		func(_ context.Context, args Args) (tools.Result, error) {
			return ws.read(args)
		},
	)
}

func schema() *tools.Schema {
	return tools.Object(map[string]*tools.Property{
		"path": {
			Type:        "string",
			Description: "File path. Relative resolves from the workspace root; absolute must be within it.",
		},
		"offset": {
			Type:        "number",
			Description: "Starting line number, 1-based (default 1). Ignored for base64.",
		},
		"limit": {
			Type:        "number",
			Description: "Maximum number of lines to return (default: all). Ignored for base64.",
		},
		"encoding": {
			Type:        "string",
			Description: "Output encoding (default utf8). Use base64 for binary files.",
			Enum:        []string{"utf8", "base64"},
		},
	}, "path")
}

func (w *Workspace) read(args Args) (tools.Result, error) {
	resolved, err := w.resolve(args.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if _, err := w.stat(resolved); err != nil {
		return tools.Result{}, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return tools.Result{}, fmt.Errorf("read file: %w", err)
	}

	switch args.Encoding {
	case "", "utf8":
		return w.readText(args, data)
	case "base64":
		enc := base64.StdEncoding.EncodeToString(data)
		return tools.Result{
			Content: enc,
			Data:    map[string]any{"path": args.Path, "encoding": "base64", "bytes": len(data)},
		}, nil
	default:
		return tools.Result{}, fmt.Errorf("unsupported encoding %q (expected utf8 or base64)", args.Encoding)
	}
}

func (w *Workspace) readText(args Args, data []byte) (tools.Result, error) {
	if !utf8.Valid(data) {
		return tools.Result{}, fmt.Errorf("file is not valid UTF-8 (binary?); retry with encoding=\"base64\"")
	}

	// Drop a single trailing newline so the last line isn't counted as empty.
	text := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	total := len(lines)

	offset := args.Offset
	if offset <= 0 {
		offset = 1
	}
	start := offset - 1 // 0-based
	if start >= total {
		return tools.Result{
			Content: fmt.Sprintf("[No lines in range, file has %d lines]", total),
			Data:    map[string]any{"path": args.Path, "lines": total},
		}, nil
	}

	end := total
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&sb, "%d: %s\n", i+1, lines[i])
	}
	if start == 0 && end == total {
		fmt.Fprintf(&sb, "[%d lines total]", total)
	} else {
		fmt.Fprintf(&sb, "[Lines %d-%d of %d]", start+1, end, total)
	}

	return tools.Result{
		Content: sb.String(),
		Data:    map[string]any{"path": args.Path, "lines": total, "from": start + 1, "to": end},
	}, nil
}
