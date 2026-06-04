// Package godhome resolves paths under god's home directory (~/.god by
// default, overridable with $GOD_HOME). It is the single source of truth for
// where god keeps its config, runtime data, and control socket, so the binary
// behaves identically regardless of the working directory it is launched from.
package godhome

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// SetEnv sets KEY=value in ~/.god/.env, replacing an existing KEY= line or
// appending a new one, and leaves all other lines (and comments) intact. It also
// updates the current process environment so the value is visible immediately.
// The file is written 0600 since it holds secrets (API keys).
func SetEnv(key, value string) error {
	dir, err := Ensure()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, ".env")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	line := key + "=" + value
	var out bytes.Buffer
	replaced := false
	sc := bufio.NewScanner(bytes.NewReader(existing))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		t := sc.Text()
		// Match "KEY=" or "export KEY=", ignoring leading whitespace.
		trimmed := strings.TrimSpace(t)
		if strings.HasPrefix(trimmed, key+"=") || strings.HasPrefix(trimmed, "export "+key+"=") {
			out.WriteString(line + "\n")
			replaced = true
			continue
		}
		out.WriteString(t + "\n")
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	if !replaced {
		out.WriteString(line + "\n")
	}

	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Setenv(key, value)
}

// AcquireGatewayLock creates and exclusively locks ~/.god/gateway.lock,
// preventing a second gateway from starting. The lock is held for the lifetime
// of the process and released automatically on exit or crash (no stale files).
// Call the returned release func in a defer to clean up on graceful shutdown.
func AcquireGatewayLock() (release func(), err error) {
	dir, err := Ensure()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "gateway.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("gateway lock: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// Read the PID written by the existing owner for a helpful error.
		if pid, e := os.ReadFile(path); e == nil && len(pid) > 0 {
			return nil, fmt.Errorf("gateway already running (pid %s) — stop it first", pid)
		}
		return nil, fmt.Errorf("gateway already running — stop it first")
	}
	// Write our PID so the error message above is useful to the next starter.
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d", os.Getpid())

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		_ = os.Remove(path)
	}, nil
}
