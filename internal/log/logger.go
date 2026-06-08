package log

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func init() {
	if os.Getenv("RILL_LOG_FORMAT") == "json" {
		defaultLogger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	} else {
		defaultLogger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
}

// Logger returns the package-level structured logger.
func Logger() *slog.Logger { return defaultLogger }
