package fs

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

func haveBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func callTool(t *testing.T, tool tools.Tool, args any) (tools.Result, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tool.Execute(context.Background(), raw)
}

// --- list_dir (pure stdlib) ---

func TestListDir(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "b.txt"), "x")
	write(t, filepath.Join(root, "sub", "c.txt"), "y")

	res, err := callTool(t, NewListDirTool(ws), ListArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "[DIR]  sub/") {
		t.Errorf("missing dir entry:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "b.txt") {
		t.Errorf("missing file entry:\n%s", res.Content)
	}
	// Directories sort before files.
	if strings.Index(res.Content, "sub/") > strings.Index(res.Content, "b.txt") {
		t.Errorf("dirs should list before files:\n%s", res.Content)
	}
}

func TestListDir_NotADir(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "f.txt"), "x")
	if _, err := callTool(t, NewListDirTool(ws), ListArgs{Path: "f.txt"}); err == nil {
		t.Fatal("listing a file must error")
	}
}

func TestListDir_Containment(t *testing.T) {
	ws, _ := newWorkspace(t)
	if _, err := callTool(t, NewListDirTool(ws), ListArgs{Path: "../.."}); err == nil {
		t.Fatal("escape must be rejected")
	}
}

// --- glob/grep argument assembly + containment (stubbed runner) ---

func TestGlob_ArgsAndContainment(t *testing.T) {
	ws, _ := newWorkspace(t)
	var gotDir, gotName string
	var gotArgs []string
	stub := func(_ context.Context, dir, name string, args []string) (string, int, error) {
		gotDir, gotName, gotArgs = dir, name, args
		return "a.go\nb.go\n", 0, nil
	}
	res, err := callTool(t, NewGlobTool(ws, stub), GlobArgs{Pattern: "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "fd" {
		t.Errorf("binary = %q, want fd", gotName)
	}
	if gotDir != ws.Root() {
		t.Errorf("dir = %q, want root %q", gotDir, ws.Root())
	}
	if gotArgs[len(gotArgs)-2] != "**/*.go" || gotArgs[len(gotArgs)-1] != "." {
		t.Errorf("expected pattern then base '.', got %v", gotArgs)
	}
	if !strings.Contains(res.Content, "a.go") {
		t.Errorf("content = %q", res.Content)
	}
}

func TestGlob_EmptyPattern(t *testing.T) {
	ws, _ := newWorkspace(t)
	stub := func(_ context.Context, _, _ string, _ []string) (string, int, error) { return "", 0, nil }
	if _, err := callTool(t, NewGlobTool(ws, stub), GlobArgs{Pattern: "  "}); err == nil {
		t.Fatal("empty pattern must error")
	}
}

func TestGlob_BadPathRejectedBeforeRun(t *testing.T) {
	ws, _ := newWorkspace(t)
	called := false
	stub := func(_ context.Context, _, _ string, _ []string) (string, int, error) {
		called = true
		return "", 0, nil
	}
	if _, err := callTool(t, NewGlobTool(ws, stub), GlobArgs{Pattern: "*", Path: "../etc"}); err == nil {
		t.Fatal("escape path must be rejected")
	}
	if called {
		t.Fatal("runner must not run for an out-of-workspace path")
	}
}

func TestGrep_ArgsAndFlags(t *testing.T) {
	ws, _ := newWorkspace(t)
	var gotArgs []string
	stub := func(_ context.Context, _, name string, args []string) (string, int, error) {
		gotArgs = args
		if name != "rg" {
			t.Errorf("binary = %q, want rg", name)
		}
		return "main.go:3:func main", 0, nil
	}
	res, err := callTool(t, NewGrepTool(ws, stub), GrepArgs{
		Pattern: "func", Glob: "*.go", CaseInsensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"--ignore-case", "--glob *.go", "--line-number", "-- func ."} {
		if !strings.Contains(joined, want) {
			t.Errorf("rg args missing %q: %v", want, gotArgs)
		}
	}
	if !strings.Contains(res.Content, "main.go:3") {
		t.Errorf("content = %q", res.Content)
	}
}

func TestGrep_NoMatchesIsNotError(t *testing.T) {
	ws, _ := newWorkspace(t)
	stub := func(_ context.Context, _, _ string, _ []string) (string, int, error) {
		return "", 1, nil // ripgrep: exit 1 = no matches
	}
	res, err := callTool(t, NewGrepTool(ws, stub), GrepArgs{Pattern: "zzz"})
	if err != nil {
		t.Fatalf("no-match should not error: %v", err)
	}
	if !strings.Contains(res.Content, "No matches") {
		t.Errorf("content = %q", res.Content)
	}
}

func TestGrep_RealError(t *testing.T) {
	ws, _ := newWorkspace(t)
	stub := func(_ context.Context, _, _ string, _ []string) (string, int, error) {
		return "", 2, nil // ripgrep: exit 2 = real error
	}
	if _, err := callTool(t, NewGrepTool(ws, stub), GrepArgs{Pattern: "("}); err == nil {
		t.Fatal("rg exit 2 must surface as error")
	}
}

func TestCapLines(t *testing.T) {
	var b strings.Builder
	for range maxFindResults + 50 {
		b.WriteString("line\n")
	}
	lines, total := capLines(b.String())
	if total != maxFindResults+50 {
		t.Errorf("total = %d, want %d", total, maxFindResults+50)
	}
	if len(lines) != maxFindResults {
		t.Errorf("capped lines = %d, want %d", len(lines), maxFindResults)
	}
	if !strings.Contains(joinWithMarker(lines, total), "showing") {
		t.Error("expected truncation marker")
	}
}

// --- live tests against the real fd/rg binaries (skip if absent) ---

func TestGlob_Live(t *testing.T) {
	if !haveBin("fd") {
		t.Skip("fd not installed")
	}
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "x.go"), "package x")
	write(t, filepath.Join(root, "sub", "y.go"), "package y")
	write(t, filepath.Join(root, "readme.md"), "# hi")

	res, err := callTool(t, NewGlobTool(ws, nil), GlobArgs{Pattern: "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "x.go") || !strings.Contains(res.Content, "y.go") {
		t.Errorf("glob missed go files:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "readme.md") {
		t.Errorf("glob should not match md:\n%s", res.Content)
	}
}

func TestGrep_Live(t *testing.T) {
	if !haveBin("rg") {
		t.Skip("rg not installed")
	}
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "a.txt"), "alpha\nbeta\nGAMMA\n")

	res, err := callTool(t, NewGrepTool(ws, nil), GrepArgs{Pattern: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "a.txt:2:beta") {
		t.Errorf("grep result unexpected:\n%s", res.Content)
	}

	res, err = callTool(t, NewGrepTool(ws, nil), GrepArgs{Pattern: "gamma", CaseInsensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "GAMMA") {
		t.Errorf("case-insensitive grep missed GAMMA:\n%s", res.Content)
	}
}
