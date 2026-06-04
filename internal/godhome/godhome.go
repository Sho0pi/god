// Package godhome resolves paths under god's home directory (~/.god by
// default, overridable with $GOD_HOME). It is the single source of truth for
// where god keeps its config, runtime data, and control socket, so the binary
// behaves identically regardless of the working directory it is launched from.
package godhome

import (
	"fmt"
	"os"
	"path/filepath"
)

// envHome is the environment variable that overrides the default home dir.
const envHome = "GOD_HOME"

// Dir returns god's home directory: $GOD_HOME if set, else ~/.god.
func Dir() (string, error) {
	if d := os.Getenv(envHome); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".god"), nil
}

// Ensure returns the home directory, creating it (0700) if absent.
func Ensure() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// Path joins elems onto the home directory without creating anything.
func Path(elems ...string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, elems...)...), nil
}

// SocketPath returns the control socket path (~/.god/god.sock). It ensures the
// home directory exists, since the listener needs the parent dir present.
func SocketPath() (string, error) {
	dir, err := Ensure()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "god.sock"), nil
}
