package fs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

func newWorkspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := New(Config{Root: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ws, ws.Root() // symlink-resolved root (macOS /var → /private/var)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func call(t *testing.T, ws *Workspace, args Args) (tools.Result, error) {
	t.Helper()
	tool := NewReadFileTool(ws)
	raw, _ := json.Marshal(args)
	return tool.Execute(context.Background(), raw)
}

func TestRead_FullFileNumbered(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "a.txt"), "one\ntwo\nthree\n")

	res, err := call(t, ws, Args{Path: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := "1: one\n2: two\n3: three\n[3 lines total]"
	if res.Content != want {
		t.Fatalf("content =\n%q\nwant\n%q", res.Content, want)
	}
}

func TestRead_OffsetLimitWindow(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "a.txt"), "l1\nl2\nl3\nl4\nl5\n")

	res, err := call(t, ws, Args{Path: "a.txt", Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := "2: l2\n3: l3\n[Lines 2-3 of 5]"
	if res.Content != want {
		t.Fatalf("content =\n%q\nwant\n%q", res.Content, want)
	}
}

func TestRead_OffsetPastEOF(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "a.txt"), "only\n")

	res, err := call(t, ws, Args{Path: "a.txt", Offset: 99})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "No lines in range") {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestRead_NestedPathOK(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "sub", "dir", "f.txt"), "hi\n")

	res, err := call(t, ws, Args{Path: "sub/dir/f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "1: hi") {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestRead_Base64(t *testing.T) {
	ws, root := newWorkspace(t)
	raw := []byte{0x00, 0xff, 0x10, 0x42} // non-UTF8 binary
	if err := os.WriteFile(filepath.Join(root, "bin"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := call(t, ws, Args{Path: "bin", Encoding: "base64"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("base64 mismatch: %q", res.Content)
	}
}

func TestRead_BinaryInUTF8Mode(t *testing.T) {
	ws, root := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := call(t, ws, Args{Path: "bin"})
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("want binary error suggesting base64, got %v", err)
	}
}

func TestRead_Containment(t *testing.T) {
	ws, _ := newWorkspace(t)
	bad := []string{
		"../etc/passwd",
		"../../etc/passwd",
		"sub/../../escape",
		"/etc/passwd",
		"a\x00b",
		"",
	}
	for _, p := range bad {
		if _, err := call(t, ws, Args{Path: p}); err == nil {
			t.Errorf("path %q: expected rejection, got nil error", p)
		}
	}
}

func TestRead_AbsoluteInsideRootOK(t *testing.T) {
	ws, root := newWorkspace(t)
	abs := filepath.Join(root, "in.txt")
	write(t, abs, "x\n")
	if _, err := call(t, ws, Args{Path: abs}); err != nil {
		t.Fatalf("absolute in-root path should work: %v", err)
	}
}

func TestRead_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on windows CI")
	}
	ws, root := newWorkspace(t)

	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside, "top secret\n")

	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	_, err := call(t, ws, Args{Path: "link.txt"})
	if err == nil {
		t.Fatal("symlink pointing outside workspace must be rejected")
	}
}

func TestRead_SizeCap(t *testing.T) {
	root := t.TempDir()
	ws, err := New(Config{Root: root, MaxReadBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(ws.Root(), "big.txt"), "way more than eight bytes")
	_, err = call(t, ws, Args{Path: "big.txt"})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want too-large error, got %v", err)
	}
}

func TestRead_DirectoryRejected(t *testing.T) {
	ws, root := newWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, ws, Args{Path: "d"}); err == nil {
		t.Fatal("reading a directory must error")
	}
}

func TestRead_NotFound(t *testing.T) {
	ws, _ := newWorkspace(t)
	if _, err := call(t, ws, Args{Path: "nope.txt"}); err == nil {
		t.Fatal("missing file must error")
	}
}

func TestNew_MissingRoot(t *testing.T) {
	if _, err := New(Config{Root: filepath.Join(t.TempDir(), "does-not-exist")}); err == nil {
		t.Fatal("New with nonexistent root must error")
	}
}

func TestNew_UnsupportedEncoding(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "a.txt"), "x\n")
	if _, err := call(t, ws, Args{Path: "a.txt", Encoding: "rot13"}); err == nil {
		t.Fatal("unsupported encoding must error")
	}
}
