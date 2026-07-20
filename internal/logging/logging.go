package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON logger that is silent unless the caller explicitly opts
// into development logging. Task content, paths, and user data are never
// added by this package; callers should log IDs and timings only.
func New(w io.Writer) *slog.Logger {
	if strings.EqualFold(os.Getenv("DT_TASK_ENV"), "development") {
		level := new(slog.LevelVar)
		switch strings.ToLower(os.Getenv("DT_TASK_LOG_LEVEL")) {
		case "debug":
			level.Set(slog.LevelDebug)
		case "warn":
			level.Set(slog.LevelWarn)
		case "error":
			level.Set(slog.LevelError)
		default:
			level.Set(slog.LevelInfo)
		}
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
