package configtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

const validCfg = `llm:
  model: gemini-3.1-flash-lite
admin:
  - "972500000000"
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "god.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exec(t *testing.T, path string, args Args) (tools.Result, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return New(path).Execute(context.Background(), raw)
}

func TestConfigGet(t *testing.T) {
	path := writeTemp(t, validCfg)
	res, err := exec(t, path, Args{Action: "get"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "972500000000") {
		t.Fatalf("get did not return current config:\n%s", res.Content)
	}
}

func TestConfigSetValidWritesAndBacksUp(t *testing.T) {
	path := writeTemp(t, validCfg)
	newCfg := validCfg + "  - \"972511111111\"\n"

	res, err := exec(t, path, Args{Action: "set", Content: newCfg})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "hot-reload") {
		t.Errorf("unexpected success message: %s", res.Content)
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
	_, err := exec(t, path, Args{Action: "set", Content: "llm:\n  model: [this is: not valid yaml"})
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	got, _ := os.ReadFile(path)
	if string(got) != validCfg {
		t.Errorf("invalid set must not modify the file, got:\n%s", got)
	}
}

func TestConfigSetMissingContent(t *testing.T) {
	path := writeTemp(t, validCfg)
	if _, err := exec(t, path, Args{Action: "set"}); err == nil {
		t.Fatal("expected error when content missing")
	}
}

func TestConfigUnknownAction(t *testing.T) {
	path := writeTemp(t, validCfg)
	if _, err := exec(t, path, Args{Action: "delete"}); err == nil {
		t.Fatal("expected error for unknown action")
	}
}
