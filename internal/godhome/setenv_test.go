package godhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempHome points GOD_HOME at a temp dir for the duration of a test.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(envHome, dir)
	return dir
}

func TestSetEnvAppendsNewKey(t *testing.T) {
	dir := withTempHome(t)
	if err := SetEnv("OPENAI_API_KEY", "sk-123"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(got), "OPENAI_API_KEY=sk-123") {
		t.Errorf("env file missing key:\n%s", got)
	}
	if os.Getenv("OPENAI_API_KEY") != "sk-123" {
		t.Error("SetEnv should also update the process environment")
	}
}

func TestSetEnvReplacesExistingKey(t *testing.T) {
	dir := withTempHome(t)
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("# my keys\nGEMINI_API_KEY=old\nOTHER=keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetEnv("GEMINI_API_KEY", "new"); err != nil {
		t.Fatal(err)
	}
	got := string(mustRead(t, path))
	if strings.Contains(got, "GEMINI_API_KEY=old") {
		t.Error("old value should be gone")
	}
	if !strings.Contains(got, "GEMINI_API_KEY=new") {
		t.Error("new value should be present")
	}
	// Unrelated lines and comments survive.
	if !strings.Contains(got, "# my keys") || !strings.Contains(got, "OTHER=keep") {
		t.Errorf("unrelated lines not preserved:\n%s", got)
	}
	// Exactly one GEMINI_API_KEY line.
	if n := strings.Count(got, "GEMINI_API_KEY="); n != 1 {
		t.Errorf("want 1 GEMINI_API_KEY line, got %d", n)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
