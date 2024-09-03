package cfg

import (
	"log/slog"
	"strings"
)

var (
	LogLevel  string
)

func SlogLevel() slog.Level {
	switch strings.ToUpper(LogLevel) {
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
