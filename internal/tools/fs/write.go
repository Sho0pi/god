package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sho0pi/god/internal/tools"
)

// containWrite validates an untrusted path for a write, allowing the leaf to not
// yet exist (write_file may create it). It contains the path to the workspace by
// resolving symlinks on the longest existing ancestor and re-checking, and it
// refuses to write through a symlink leaf or over a directory.
func (w *Workspace) containWrite(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsRune(p, '\x00') {
		return "", fmt.Errorf("path not allowed: contains null byte")
	}
	if hasDotDot(p) {
		return "", fmt.Errorf("path not allowed: %q contains %q", p, "..")
	}

	cleaned := filepath.Clean(p)
	joined := cleaned
	if !filepath.IsAbs(cleaned) {
		joined = filepath.Join(w.root, cleaned)
	}
	joined = filepath.Clean(joined)
	if !w.within(joined) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}

	// Resolve symlinks on the longest existing ancestor to catch a symlinked
	// directory that points outside the workspace.
	anc := joined
	for {
		if _, err := os.Lstat(anc); err == nil {
			break
		}
		parent := filepath.Dir(anc)
		if parent == anc {
			break
		}
		anc = parent
	}
	realAnc, err := filepath.EvalSymlinks(anc)
	if err != nil {
		return "", err
	}
	if !w.within(realAnc) {
		return "", fmt.Errorf("path escapes workspace via symlink: %s", p)
	}
	rel, err := filepath.Rel(anc, joined)
	if err != nil {
		return "", err
	}
	final := filepath.Join(realAnc, rel)
	if !w.within(final) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}

	if fi, err := os.Lstat(final); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to write through symlink: %s", p)
		}
		if fi.IsDir() {
			return "", fmt.Errorf("path is a directory: %s", p)
		}
	}
	return final, nil
}

// hasDotDot reports whether any path segment is "..".
func hasDotDot(p string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(p), "/"), "..")
}

// --- write_file ---

// WriteArgs are the write_file arguments.
type WriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// NewWriteFileTool returns the write_file tool. It creates or overwrites a file,
// creating parent directories inside the workspace as needed.
func NewWriteFileTool(ws *Workspace) tools.Tool {
	return tools.NewTypedTool(
		"write_file",
		"Create or overwrite a file with the given content. Parent directories are "+
			"created as needed. Path is relative to the workspace root; absolute paths "+
			"must be inside it. Overwrites existing files.",
		tools.Object(map[string]*tools.Property{
			"path":    {Type: "string", Description: "File path to write, relative to the workspace root."},
			"content": {Type: "string", Description: "Full file content to write."},
		}, "path", "content"),
		func(_ context.Context, args WriteArgs) (tools.Result, error) {
			return ws.writeFile(args)
		},
	)
}

func (w *Workspace) writeFile(args WriteArgs) (tools.Result, error) {
	final, err := w.containWrite(args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	_, statErr := os.Stat(final)
	existed := statErr == nil

	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return tools.Result{}, fmt.Errorf("create parent dirs: %w", err)
	}
	if err := os.WriteFile(final, []byte(args.Content), 0o644); err != nil {
		return tools.Result{}, fmt.Errorf("write file: %w", err)
	}

	verb := "Created"
	if existed {
		verb = "Overwrote"
	}
	return tools.Result{
		Content: fmt.Sprintf("%s %s (%d bytes).", verb, args.Path, len(args.Content)),
		Data:    map[string]any{"path": args.Path, "bytes": len(args.Content), "created": !existed},
	}, nil
}

// --- edit_file ---

// EditArgs are the edit_file arguments.
type EditArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// NewEditFileTool returns the edit_file tool: an exact string replacement in an
// existing file. old_string must be present and unique unless replace_all is set.
func NewEditFileTool(ws *Workspace) tools.Tool {
	return tools.NewTypedTool(
		"edit_file",
		"Replace an exact string in an existing file. old_string must match exactly "+
			"and be unique unless replace_all is true. Use enough surrounding context to "+
			"make old_string unique. Path is relative to the workspace root.",
		tools.Object(map[string]*tools.Property{
			"path":        {Type: "string", Description: "File to edit, relative to the workspace root."},
			"old_string":  {Type: "string", Description: "Exact text to replace (include surrounding context to be unique)."},
			"new_string":  {Type: "string", Description: "Replacement text."},
			"replace_all": {Type: "boolean", Description: "Replace every occurrence instead of requiring a unique match (default false)."},
		}, "path", "old_string", "new_string"),
		func(_ context.Context, args EditArgs) (tools.Result, error) {
			return ws.editFile(args)
		},
	)
}

func (w *Workspace) editFile(args EditArgs) (tools.Result, error) {
	if args.OldString == "" {
		return tools.Result{}, fmt.Errorf("old_string is required")
	}
	if args.OldString == args.NewString {
		return tools.Result{}, fmt.Errorf("old_string and new_string are identical; nothing to change")
	}

	// resolve() requires existence and contains symlinks — edit only touches
	// existing, in-workspace files.
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
	content := string(data)

	count := strings.Count(content, args.OldString)
	switch {
	case count == 0:
		return tools.Result{}, fmt.Errorf("old_string not found in %s", args.Path)
	case count > 1 && !args.ReplaceAll:
		return tools.Result{}, fmt.Errorf("old_string is not unique in %s (%d matches); add context or set replace_all", args.Path, count)
	}

	var updated string
	if args.ReplaceAll {
		updated = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		updated = strings.Replace(content, args.OldString, args.NewString, 1)
	}

	if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
		return tools.Result{}, fmt.Errorf("write file: %w", err)
	}

	replaced := 1
	if args.ReplaceAll {
		replaced = count
	}
	return tools.Result{
		Content: fmt.Sprintf("Edited %s (%d replacement(s)).", args.Path, replaced),
		Data:    map[string]any{"path": args.Path, "replacements": replaced},
	}, nil
}
