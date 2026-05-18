package logging

import (
	"context"
	"log/slog"
)

// ---------------------------------------------------------------------------
// Error logging helpers
// ---------------------------------------------------------------------------

// LogIfErr logs err at WARN level if non-nil, using the context-enriched
// logger. Use for best-effort operations where the error cannot be returned
// but should be visible (e.g. event emission, message send, state persistence).
func LogIfErr(ctx context.Context, err error, msg string, attrs ...slog.Attr) {
	if err == nil {
		return
	}
	logErr(FromContext(ctx), slog.LevelWarn, err, msg, attrs)
}

// DebugIfErr logs err at DEBUG level if non-nil, using the default logger.
// Use for best-effort cleanup operations where failure is expected or harmless
// (e.g. Close, Remove, RemoveAll).
func DebugIfErr(err error, msg string, attrs ...slog.Attr) {
	if err == nil {
		return
	}
	logErr(slog.Default(), slog.LevelDebug, err, msg, attrs)
}

func logErr(l *slog.Logger, level slog.Level, err error, msg string, attrs []slog.Attr) {
	args := make([]any, 0, len(attrs)+1)
	args = append(args, slog.Any("error", err))
	for _, a := range attrs {
		args = append(args, a)
	}
	l.Log(context.Background(), level, msg, args...)
}
