// Package logging configures god's process-wide slog logger. It writes
// human-readable text to stderr (for live/interactive use) and, when the home
// directory is available, structured JSON with source locations to
// ~/.god/god.log (for after-the-fact debugging — grep/jq friendly, every line
// carries file:line and full key/value context).
package logging

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sho0pi/god/internal/godhome"
)

// Setup installs the default logger: text→stderr (Info) plus, if possible,
// JSON→~/.god/god.log (Debug, with source). It returns the log file path that
// was opened, or "" if only stderr logging is active.
func Setup() string {
	handlers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
	}

	logPath := ""
	if dir, err := godhome.Ensure(); err == nil {
		path := filepath.Join(dir, "god.log")
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			// File gets more detail than the console: Debug level + source so an
			// error in the log points straight at the code that emitted it.
			handlers = append(handlers, slog.NewJSONHandler(f, &slog.HandlerOptions{
				Level:     slog.LevelDebug,
				AddSource: true,
			}))
			logPath = path
		}
	}

	slog.SetDefault(slog.New(Fanout(handlers...)))
	return logPath
}

// Fanout returns a slog.Handler that forwards every record to all of hs. It
// exists because stdlib has no multi-handler before Go 1.26, and we want a
// different format per sink (text on stderr, JSON in the file).
func Fanout(hs ...slog.Handler) slog.Handler {
	return fanout(hs)
}

type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Clone so each handler can mutate its copy independently.
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
