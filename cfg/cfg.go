package cfg

import (
	"log/slog"
	"strings"
)

var (
	FlagLogLevel string
	// MaxLineLen is the maximum line length for code generation.
	// Prompt bodies are not wrapped.
	MaxLineLen int
)

func LogLevel() slog.Level {
	switch strings.ToUpper(FlagLogLevel) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func TestMode() {
	MaxLineLen = 80
	FlagLogLevel = "DEBUG"
}
