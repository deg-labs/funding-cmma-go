// Package logging はログレベルに応じて出力先を分ける slog ハンドラーを提供します。
//
//	DEBUG / INFO  -> stdout
//	WARN / ERROR  -> stderr
package logging

import (
	"context"
	"log/slog"
	"os"
)

// splitHandler はログレベルに応じて stdout と stderr を切り替えます。
type splitHandler struct {
	stdout slog.Handler
	stderr slog.Handler
}

// New は指定した形式のハンドラーを返します。format は "json" または "text" です。
func New(level slog.Level, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	var stdout, stderr slog.Handler
	if format == "text" {
		stdout = slog.NewTextHandler(os.Stdout, opts)
		stderr = slog.NewTextHandler(os.Stderr, opts)
	} else {
		stdout = slog.NewJSONHandler(os.Stdout, opts)
		stderr = slog.NewJSONHandler(os.Stderr, opts)
	}
	return &splitHandler{stdout: stdout, stderr: stderr}
}

func (h *splitHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.stdout.Enabled(ctx, level) || h.stderr.Enabled(ctx, level)
}

func (h *splitHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelWarn {
		return h.stderr.Handle(ctx, record)
	}
	return h.stdout.Handle(ctx, record)
}

func (h *splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &splitHandler{
		stdout: h.stdout.WithAttrs(attrs),
		stderr: h.stderr.WithAttrs(attrs),
	}
}

func (h *splitHandler) WithGroup(name string) slog.Handler {
	return &splitHandler{
		stdout: h.stdout.WithGroup(name),
		stderr: h.stderr.WithGroup(name),
	}
}

// ParseLevel は LOG_LEVEL の文字列を slog.Level に変換します。
func ParseLevel(raw string) (slog.Level, error) {
	switch raw {
	case "", "info", "INFO":
		return slog.LevelInfo, nil
	case "debug", "DEBUG":
		return slog.LevelDebug, nil
	case "warn", "WARN", "warning":
		return slog.LevelWarn, nil
	case "error", "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, &invalidLevelError{raw: raw}
	}
}

type invalidLevelError struct{ raw string }

func (e *invalidLevelError) Error() string {
	return "invalid LOG_LEVEL: " + e.raw
}
