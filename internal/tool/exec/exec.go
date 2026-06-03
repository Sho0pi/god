// Package exec provides an LLM-callable shell tool, mirroring OpenClaw's exec
// tool. Unlike OpenClaw's host-level shell, every command here runs inside a
// locked-down, throwaway Docker container: no network (by default), no host
// filesystem, read-only root, dropped capabilities, and memory/CPU/pid/time
// limits. This contains prompt-injection: even if the model is tricked into
// running a destructive command, the blast radius is a container that is
// deleted the moment the command returns.
//
// This tool must only be granted to trusted roles (see role `tools` in
// god.yaml). It is disabled by default.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sho0pi/god/internal/tool"
)

const (
	maxOutputBytes = 4000
	defaultImage   = "alpine:3.20"
	defaultTimeout = 30 * time.Second
)

// Config configures the sandbox. Zero values fall back to safe defaults.
type Config struct {
	Image     string
	Timeout   time.Duration
	Memory    string // docker --memory, e.g. "256m"
	CPUs      string // docker --cpus, e.g. "0.5"
	PidsLimit int    // docker --pids-limit
	Network   bool   // false → --network=none
}

// Tool runs shell commands inside an ephemeral sandboxed container.
type Tool struct {
	docker string // resolved path to the docker binary
	cfg    Config
}

// New resolves docker and returns the tool, or an error if docker is absent.
func New(cfg Config) (*Tool, error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	if cfg.Image == "" {
		cfg.Image = defaultImage
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Memory == "" {
		cfg.Memory = "256m"
	}
	if cfg.CPUs == "" {
		cfg.CPUs = "0.5"
	}
	if cfg.PidsLimit <= 0 {
		cfg.PidsLimit = 128
	}
	return &Tool{docker: docker, cfg: cfg}, nil
}

func (t *Tool) Name() string { return "exec" }

func (t *Tool) Description() string {
	return "Run a shell command inside a sandboxed, throwaway Linux container " +
		"(image " + t.cfg.Image + "). The container has no access to the host " +
		"filesystem and is destroyed after the command returns; files do not " +
		"persist between calls. Network is " + networkWord(t.cfg.Network) + ". " +
		"Returns combined stdout+stderr and the exit code. Use for calculations, " +
		"scripting, and inspecting command output — not for changing the host."
}

func (t *Tool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"command": {
				Type:        "string",
				Description: "Shell command to run, e.g. \"python3 -c 'print(2**10)'\" or \"echo hi | wc -c\".",
			},
			"timeout_seconds": {
				Type:        "number",
				Description: fmt.Sprintf("Optional wall-clock limit (default %d, max %d).", int(t.cfg.Timeout.Seconds()), int(t.cfg.Timeout.Seconds())),
			},
		},
		Required: []string{"command"},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := t.cfg.Timeout
	if n, ok := args["timeout_seconds"].(float64); ok && n > 0 {
		if req := time.Duration(n) * time.Second; req < timeout {
			timeout = req // callers may shorten, never extend past the configured cap
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := fmt.Sprintf("god-exec-%d", time.Now().UnixNano())
	dockerArgs := []string{
		"run", "--rm",
		"--name", name,
		"--read-only",
		"--tmpfs", "/tmp:rw,size=64m,mode=1777",
		"--memory", t.cfg.Memory, "--memory-swap", t.cfg.Memory,
		"--cpus", t.cfg.CPUs,
		"--pids-limit", fmt.Sprintf("%d", t.cfg.PidsLimit),
		"--user", "nobody",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"-w", "/tmp",
	}
	if !t.cfg.Network {
		dockerArgs = append(dockerArgs, "--network", "none")
	}
	dockerArgs = append(dockerArgs, t.cfg.Image, "sh", "-c", command)

	var out bytes.Buffer
	cmd := exec.CommandContext(runCtx, t.docker, dockerArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()

	// On timeout, the docker client is killed but the container may linger —
	// force-remove it so it can't keep consuming resources.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		t.forceRemove(name)
		body := truncate(out.String())
		return fmt.Sprintf("timed out after %s (container killed)\n%s", timeout, body), nil
	}

	body := truncate(out.String())
	exitCode := 0
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		exitCode = ee.ExitCode()
	} else if runErr != nil {
		// Failure to even launch the sandbox (e.g. image missing). Surface it.
		return "", fmt.Errorf("sandbox launch failed: %w: %s", runErr, body)
	}

	return fmt.Sprintf("exit code: %d\n%s", exitCode, body), nil
}

func (t *Tool) forceRemove(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, t.docker, "rm", "-f", name).Run()
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + "\n...[output truncated]"
}

func networkWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
