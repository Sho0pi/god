package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleYAML = `# top comment
llm:
  model: gemini-3.1-flash-lite  # inline comment

connectors:
  whatsapp:
    enabled: true
    # keep this list
    allow: []
  telegram:
    enabled: false
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "god.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetValuesUpdatesExistingKey(t *testing.T) {
	path := writeTemp(t, sampleYAML)
	if err := SetValues(path, map[string]any{"connectors.telegram.enabled": true}); err != nil {
		t.Fatalf("SetValues: %v", err)
	}
	out, _ := os.ReadFile(path)
	cfg, err := Parse(out)
	if err != nil {
		t.Fatalf("result does not parse: %v", err)
	}
	if !cfg.Connectors.Telegram.Enabled {
		t.Error("telegram.enabled should be true after edit")
	}
	// Unrelated values and comments must survive.
	s := string(out)
	if !strings.Contains(s, "# top comment") || !strings.Contains(s, "# inline comment") || !strings.Contains(s, "# keep this list") {
		t.Errorf("comments not preserved:\n%s", s)
	}
	if cfg.LLM.Model != "gemini-3.1-flash-lite" {
		t.Error("unrelated llm.model changed")
	}
}

func TestSetValuesCreatesMissingKeys(t *testing.T) {
	path := writeTemp(t, sampleYAML)
	if err := SetValues(path, map[string]any{"connectors.telegram.token": "123:ABC"}); err != nil {
		t.Fatalf("SetValues: %v", err)
	}
	out, _ := os.ReadFile(path)
	cfg, err := Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Connectors.Telegram.Token != "123:ABC" {
		t.Errorf("token = %q, want 123:ABC", cfg.Connectors.Telegram.Token)
	}
}

func TestSetValuesCreatesDeepPath(t *testing.T) {
	// Start with a file that has no telegram block at all.
	path := writeTemp(t, "llm:\n  model: x\n")
	if err := SetValues(path, map[string]any{"connectors.telegram.enabled": true}); err != nil {
		t.Fatalf("SetValues: %v", err)
	}
	out, _ := os.ReadFile(path)
	cfg, err := Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.Connectors.Telegram.Enabled {
		t.Error("deep path connectors.telegram.enabled not created")
	}
}

func TestSetValuesWritesBackup(t *testing.T) {
	path := writeTemp(t, sampleYAML)
	if err := SetValues(path, map[string]any{"connectors.telegram.enabled": true}); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	if string(bak) != sampleYAML {
		t.Error("backup should hold the original content")
	}
}

func TestSetValuesMultipleKeys(t *testing.T) {
	path := writeTemp(t, sampleYAML)
	if err := SetValues(path, map[string]any{
		"llm.provider": "openai",
		"llm.model":    "gpt-4o-mini",
	}); err != nil {
		t.Fatalf("SetValues: %v", err)
	}
	out, _ := os.ReadFile(path)
	cfg, err := Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.LLM.Provider != "openai" || cfg.LLM.Model != "gpt-4o-mini" {
		t.Errorf("llm = %+v, want openai/gpt-4o-mini", cfg.LLM)
	}
}

func TestSetValuesMissingFile(t *testing.T) {
	err := SetValues(filepath.Join(t.TempDir(), "absent.yaml"), map[string]any{"a.b": 1})
	if err == nil {
		t.Error("expected error for missing file")
	}
}
