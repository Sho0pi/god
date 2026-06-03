package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- write_file ---

func TestWriteFile_CreateAndOverwrite(t *testing.T) {
	ws, root := newWorkspace(t)

	res, err := callTool(t, NewWriteFileTool(ws), WriteArgs{Path: "new/dir/f.txt", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "Created") {
		t.Errorf("expected Created, got %q", res.Content)
	}
	got, err := os.ReadFile(filepath.Join(root, "new", "dir", "f.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("file not written: %q err=%v", got, err)
	}

	res, err = callTool(t, NewWriteFileTool(ws), WriteArgs{Path: "new/dir/f.txt", Content: "world"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "Overwrote") {
		t.Errorf("expected Overwrote, got %q", res.Content)
	}
	got, _ = os.ReadFile(filepath.Join(root, "new", "dir", "f.txt"))
	if string(got) != "world" {
		t.Fatalf("overwrite failed: %q", got)
	}
}

func TestWriteFile_Containment(t *testing.T) {
	ws, _ := newWorkspace(t)
	bad := []string{"../escape.txt", "../../etc/x", "/etc/passwd", "a\x00b", ""}
	for _, p := range bad {
		if _, err := callTool(t, NewWriteFileTool(ws), WriteArgs{Path: p, Content: "x"}); err == nil {
			t.Errorf("path %q: expected rejection", p)
		}
	}
}

func TestWriteFile_RefuseSymlinkLeaf(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on windows")
	}
	ws, root := newWorkspace(t)
	outside := filepath.Join(t.TempDir(), "target.txt")
	write(t, outside, "orig")
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(t, NewWriteFileTool(ws), WriteArgs{Path: "link.txt", Content: "pwned"}); err == nil {
		t.Fatal("must refuse writing through a symlink")
	}
	if got, _ := os.ReadFile(outside); string(got) != "orig" {
		t.Fatalf("symlink target was modified: %q", got)
	}
}

func TestWriteFile_RefuseSymlinkedDirEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on windows")
	}
	ws, root := newWorkspace(t)
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	// Writing under a symlinked-out directory must be rejected.
	if _, err := callTool(t, NewWriteFileTool(ws), WriteArgs{Path: "out/x.txt", Content: "x"}); err == nil {
		t.Fatal("must reject write through symlinked-out directory")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "x.txt")); err == nil {
		t.Fatal("file escaped workspace via symlinked dir")
	}
}

func TestWriteFile_OverDirectory(t *testing.T) {
	ws, root := newWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(t, NewWriteFileTool(ws), WriteArgs{Path: "d", Content: "x"}); err == nil {
		t.Fatal("writing over a directory must error")
	}
}

// --- edit_file ---

func TestEditFile_UniqueReplace(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "f.txt"), "alpha beta gamma")

	res, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "f.txt", OldString: "beta", NewString: "DELTA"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "1 replacement") {
		t.Errorf("content = %q", res.Content)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "alpha DELTA gamma" {
		t.Fatalf("edit result = %q", got)
	}
}

func TestEditFile_NotFound(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "f.txt"), "abc")
	if _, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "f.txt", OldString: "xyz", NewString: "q"}); err == nil {
		t.Fatal("missing old_string must error")
	}
}

func TestEditFile_NonUniqueRequiresReplaceAll(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "f.txt"), "x x x")

	if _, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "f.txt", OldString: "x", NewString: "y"}); err == nil {
		t.Fatal("non-unique match without replace_all must error")
	}

	res, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "f.txt", OldString: "x", NewString: "y", ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "3 replacement") {
		t.Errorf("content = %q", res.Content)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "y y y" {
		t.Fatalf("replace_all result = %q", got)
	}
}

func TestEditFile_IdenticalStrings(t *testing.T) {
	ws, root := newWorkspace(t)
	write(t, filepath.Join(root, "f.txt"), "abc")
	if _, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "f.txt", OldString: "a", NewString: "a"}); err == nil {
		t.Fatal("identical old/new must error")
	}
}

func TestEditFile_MissingFile(t *testing.T) {
	ws, _ := newWorkspace(t)
	if _, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "nope.txt", OldString: "a", NewString: "b"}); err == nil {
		t.Fatal("editing a missing file must error")
	}
}

func TestEditFile_Containment(t *testing.T) {
	ws, _ := newWorkspace(t)
	if _, err := callTool(t, NewEditFileTool(ws), EditArgs{Path: "../x", OldString: "a", NewString: "b"}); err == nil {
		t.Fatal("escape must be rejected")
	}
}
