package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestFanoutForwardsToAll verifies a record reaches every sink, each rendering
// in its own format, and that attrs added via the logger propagate.
func TestFanoutForwardsToAll(t *testing.T) {
	var textBuf, jsonBuf bytes.Buffer
	h := Fanout(
		slog.NewTextHandler(&textBuf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewJSONHandler(&jsonBuf, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	log := slog.New(h).With("component", "test")
	log.Info("hello", "n", 1)

	text := textBuf.String()
	js := jsonBuf.String()
	if !strings.Contains(text, "hello") || !strings.Contains(text, "component=test") {
		t.Errorf("text sink missing data: %q", text)
	}
	if !strings.Contains(js, `"msg":"hello"`) || !strings.Contains(js, `"component":"test"`) {
		t.Errorf("json sink missing data: %q", js)
	}
}

// TestFanoutRespectsPerHandlerLevel: a Debug record reaches a Debug sink but not
// an Info sink (mirrors stderr=Info, file=Debug in Setup).
func TestFanoutRespectsPerHandlerLevel(t *testing.T) {
	var infoBuf, debugBuf bytes.Buffer
	h := Fanout(
		slog.NewTextHandler(&infoBuf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewTextHandler(&debugBuf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)
	slog.New(h).Debug("trace detail")

	if infoBuf.Len() != 0 {
		t.Errorf("info-level sink should drop debug, got: %q", infoBuf.String())
	}
	if !strings.Contains(debugBuf.String(), "trace detail") {
		t.Errorf("debug-level sink should keep debug, got: %q", debugBuf.String())
	}
}

func TestFanoutEnabled(t *testing.T) {
	h := Fanout(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("should be disabled below the only handler's level")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("should be enabled at/above the handler's level")
	}
}
