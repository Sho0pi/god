package cfgtool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validCfg = `llm:
  model: gemini-3.1-flash-lite
admin:
  - "972500000000"
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "god.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigGet(t *testing.T) {
	path := writeTemp(t, validCfg)
	out, err := New(path).Execute(context.Background(), map[string]any{"action": "get"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "972500000000") {
		t.Fatalf("get did not return current config:\n%s", out)
	}
}

func TestConfigSetValidWritesAndBacksUp(t *testing.T) {
	path := writeTemp(t, validCfg)
	newCfg := validCfg + "  - \"972511111111\"\n"

	out, err := New(path).Execute(context.Background(), map[string]any{"action": "set", "content": newCfg})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hot-reload") {
		t.Errorf("unexpected success message: %s", out)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "972511111111") {
		t.Errorf("new config not written:\n%s", got)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup not created: %v", err)
	}
	if strings.Contains(string(bak), "972511111111") {
		t.Errorf("backup should hold the OLD config, got new:\n%s", bak)
	}
}

func TestConfigSetInvalidRejected(t *testing.T) {
	path := writeTemp(t, validCfg)
	_, err := New(path).Execute(context.Background(), map[string]any{
		"action":  "set",
		"content": "llm:\n  model: [this is: not valid yaml",
	})
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	// Original file must be untouched.
	got, _ := os.ReadFile(path)
	if string(got) != validCfg {
		t.Errorf("invalid set must not modify the file, got:\n%s", got)
	}
}

func TestConfigSetMissingContent(t *testing.T) {
	path := writeTemp(t, validCfg)
	if _, err := New(path).Execute(context.Background(), map[string]any{"action": "set"}); err == nil {
		t.Fatal("expected error when content missing")
	}
}
