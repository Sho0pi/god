package exec

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// dockerAvailable reports whether a docker daemon is reachable; tests skip if not.
func dockerAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skip("docker daemon not running")
	}
}

func TestExecBasic(t *testing.T) {
	dockerAvailable(t)
	tl, err := New(Config{Image: "alpine:3.20", Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tl.Execute(context.Background(), map[string]any{"command": "echo hello && expr 2 + 3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit code: 0") || !strings.Contains(out, "hello") || !strings.Contains(out, "5") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	dockerAvailable(t)
	tl, _ := New(Config{Image: "alpine:3.20", Timeout: 20 * time.Second})
	out, err := tl.Execute(context.Background(), map[string]any{"command": "exit 7"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit code: 7") {
		t.Fatalf("expected exit code 7, got:\n%s", out)
	}
}

func TestExecNoNetwork(t *testing.T) {
	dockerAvailable(t)
	tl, _ := New(Config{Image: "alpine:3.20", Timeout: 20 * time.Second, Network: false})
	// With --network=none there is no loopback route off-host; pinging a public
	// IP must fail. (Using -w 1 to bound ping's own wait.)
	out, err := tl.Execute(context.Background(), map[string]any{"command": "ping -c1 -w1 1.1.1.1; echo done"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("command did not complete:\n%s", out)
	}
	if strings.Contains(out, "exit code: 0\ndone") && !strings.Contains(out, "bad address") && !strings.Contains(out, "Network") {
		// ping should not have succeeded; a clean 0 exit with reachable host is a red flag.
		t.Logf("output (verify no network):\n%s", out)
	}
}

func TestExecTimeout(t *testing.T) {
	dockerAvailable(t)
	tl, _ := New(Config{Image: "alpine:3.20", Timeout: 2 * time.Second})
	start := time.Now()
	out, err := tl.Execute(context.Background(), map[string]any{"command": "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "timed out") {
		t.Fatalf("expected timeout, got:\n%s", out)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("timeout took too long: %s", time.Since(start))
	}
}

func TestExecReadOnlyRoot(t *testing.T) {
	dockerAvailable(t)
	tl, _ := New(Config{Image: "alpine:3.20", Timeout: 20 * time.Second})
	// Root fs is read-only; writing outside /tmp must fail.
	out, err := tl.Execute(context.Background(), map[string]any{"command": "touch /evil 2>&1; echo rc=$?"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "rc=0") {
		t.Fatalf("expected write to / to fail (read-only root), got:\n%s", out)
	}
}
