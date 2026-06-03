// Package fs holds filesystem tools confined to a configured workspace root.
// Every path argument is untrusted (it originates from the LLM, which ingests
// attacker-controlled web_search/web_extract results and message text), so all
// paths are contained to the workspace root before any filesystem access.
package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultMaxReadBytes caps a single read so a huge file can't be dumped into the
// model context. Mirrors zeroclaw's 10 MiB MAX_FILE_SIZE_BYTES.
const defaultMaxReadBytes int64 = 10 * 1024 * 1024

// Config configures a Workspace.
type Config struct {
	Root         string // directory tools may touch (empty → current working dir)
	MaxReadBytes int64  // per-read byte cap (0 → default)
}

// Workspace resolves and contains paths under an absolute, symlink-evaluated
// root.
type Workspace struct {
	root     string // absolute, symlinks resolved
	maxBytes int64
}

// New builds a Workspace. The root must exist (it is symlink-resolved once here
// and all later checks compare against the result).
func New(cfg Config) (*Workspace, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("fs root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("fs root %q: %w", abs, err)
	}
	maxBytes := cfg.MaxReadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxReadBytes
	}
	return &Workspace{root: resolved, maxBytes: maxBytes}, nil
}

// Root returns the absolute workspace root.
func (w *Workspace) Root() string { return w.root }

// resolve validates an untrusted path and returns the absolute, symlink-resolved
// path inside the workspace, or an error if it escapes containment or does not
// exist. The file must exist (EvalSymlinks requires it), which suits read_file.
func (w *Workspace) resolve(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(p, '\x00') {
		return "", errors.New("path not allowed: contains null byte")
	}
	// Defense in depth: reject explicit parent-directory segments up front, even
	// though the containment check below would also catch an escape.
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

	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}
	if !w.within(resolved) {
		return "", fmt.Errorf("path escapes workspace via symlink: %s", p)
	}
	return resolved, nil
}

// within reports whether absolute path p is inside the workspace root.
func (w *Workspace) within(p string) bool {
	rel, err := filepath.Rel(w.root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// stat returns file info for a resolved path, enforcing the size cap and
// rejecting non-regular files (directories, devices).
func (w *Workspace) stat(resolved string) (os.FileInfo, error) {
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file")
	}
	if info.Size() > w.maxBytes {
		return nil, fmt.Errorf("file too large: %d bytes (limit: %d bytes)", info.Size(), w.maxBytes)
	}
	return info, nil
}
