package fs

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sho0pi/god/internal/tools"
)

// maxFindResults caps glob/grep output lines so a broad query can't flood the
// model context.
const maxFindResults = 200

// Runner executes an external command in dir and returns its stdout and exit
// code. Injected so tests don't need fd/rg installed. A negative exit code (and
// non-nil err) means the process could not start (e.g. binary missing).
type Runner func(ctx context.Context, dir, name string, args []string) (stdout string, code int, err error)

func execRunner(ctx context.Context, dir, name string, args []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String(), ee.ExitCode(), nil // ran, non-zero exit
		}
		return "", -1, err // failed to start (binary missing, etc.)
	}
	return out.String(), 0, nil
}

// searchBase validates an untrusted path arg and returns it relative to the
// workspace root (default "."), so commands run with Dir=root produce
// root-relative output and cannot escape containment.
func (w *Workspace) searchBase(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return ".", nil
	}
	resolved, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.root, resolved)
	if err != nil {
		return "", err
	}
	return rel, nil
}

// --- glob (fd) ---

// GlobArgs are the glob arguments.
type GlobArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

// NewGlobTool returns the glob tool, backed by fd. Pass nil runner for the real
// fd binary.
func NewGlobTool(ws *Workspace, runner Runner) tools.Tool {
	if runner == nil {
		runner = execRunner
	}
	return tools.NewTypedTool(
		"glob",
		"Find files by glob pattern (e.g. '**/*.go', 'cmd/*.ts'), backed by fd. "+
			"Searches the workspace root by default, or under `path`. Respects "+
			".gitignore and does not follow symlinks. Returns matching file paths.",
		tools.Object(map[string]*tools.Property{
			"pattern": {Type: "string", Description: "Glob pattern to match file paths, e.g. '**/*.go'."},
			"path":    {Type: "string", Description: "Directory to search within (default: workspace root)."},
		}, "pattern"),
		func(ctx context.Context, args GlobArgs) (tools.Result, error) {
			return ws.glob(ctx, runner, args)
		},
	)
}

func (w *Workspace) glob(ctx context.Context, runner Runner, args GlobArgs) (tools.Result, error) {
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return tools.Result{}, fmt.Errorf("pattern is required")
	}
	base, err := w.searchBase(args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	// fd --glob --type f --color=never --no-follow -- <pattern> <base>
	out, code, err := runner(ctx, w.root, "fd",
		[]string{"--glob", "--type", "f", "--color=never", "--no-follow", "--", pattern, base})
	if err != nil {
		return tools.Result{}, fmt.Errorf("fd not available: %w", err)
	}
	if code != 0 {
		return tools.Result{}, fmt.Errorf("fd exited %d", code)
	}

	lines, total := capLines(out)
	if total == 0 {
		return tools.Result{Content: fmt.Sprintf("No files match %q.", pattern)}, nil
	}
	return tools.Result{
		Content: joinWithMarker(lines, total),
		Data:    map[string]any{"pattern": pattern, "matches": total},
	}, nil
}

// --- grep (ripgrep) ---

// GrepArgs are the grep arguments.
type GrepArgs struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Glob            string `json:"glob"`
	CaseInsensitive bool   `json:"case_insensitive"`
}

// NewGrepTool returns the grep tool, backed by ripgrep. Pass nil runner for the
// real rg binary.
func NewGrepTool(ws *Workspace, runner Runner) tools.Tool {
	if runner == nil {
		runner = execRunner
	}
	return tools.NewTypedTool(
		"grep",
		"Search file contents by regular expression, backed by ripgrep. Searches the "+
			"workspace root by default, or under `path`. Optionally restrict to files "+
			"matching `glob` (e.g. '*.go'). Returns matching lines as 'path:line:text'. "+
			"Respects .gitignore and does not follow symlinks.",
		tools.Object(map[string]*tools.Property{
			"pattern":          {Type: "string", Description: "Regular expression to search for (ripgrep syntax)."},
			"path":             {Type: "string", Description: "File or directory to search (default: workspace root)."},
			"glob":             {Type: "string", Description: "Only search files matching this glob, e.g. '*.go'."},
			"case_insensitive": {Type: "boolean", Description: "Case-insensitive search (default false)."},
		}, "pattern"),
		func(ctx context.Context, args GrepArgs) (tools.Result, error) {
			return ws.grep(ctx, runner, args)
		},
	)
}

func (w *Workspace) grep(ctx context.Context, runner Runner, args GrepArgs) (tools.Result, error) {
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return tools.Result{}, fmt.Errorf("pattern is required")
	}
	base, err := w.searchBase(args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	rgArgs := []string{"--line-number", "--no-heading", "--color=never", "--no-follow"}
	if args.CaseInsensitive {
		rgArgs = append(rgArgs, "--ignore-case")
	}
	if g := strings.TrimSpace(args.Glob); g != "" {
		rgArgs = append(rgArgs, "--glob", g)
	}
	rgArgs = append(rgArgs, "--", pattern, base)

	out, code, err := runner(ctx, w.root, "rg", rgArgs)
	if err != nil {
		return tools.Result{}, fmt.Errorf("ripgrep (rg) not available: %w", err)
	}
	switch code {
	case 0: // matches found
	case 1: // ripgrep: no matches
		return tools.Result{Content: fmt.Sprintf("No matches for %q.", pattern)}, nil
	default:
		return tools.Result{}, fmt.Errorf("rg exited %d", code)
	}

	lines, total := capLines(out)
	return tools.Result{
		Content: joinWithMarker(lines, total),
		Data:    map[string]any{"pattern": pattern, "matches": total},
	}, nil
}

// capLines splits output into lines (dropping empties), returning up to
// maxFindResults of them plus the total count.
func capLines(out string) (lines []string, total int) {
	for l := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if l == "" {
			continue
		}
		total++
		if len(lines) < maxFindResults {
			lines = append(lines, l)
		}
	}
	return lines, total
}

func joinWithMarker(lines []string, total int) string {
	s := strings.Join(lines, "\n")
	if total > len(lines) {
		s += fmt.Sprintf("\n[showing %d of %d results]", len(lines), total)
	}
	return s
}
