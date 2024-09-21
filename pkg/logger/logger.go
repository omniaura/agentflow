package logger

import (
	"log/slog"

	"github.com/ditto-assistant/agentflow/cfg"
)

func Setup() {
	SetupLevel(cfg.LogLevel())
}

func SetupLevel(lvl slog.Level) {
	slog.SetLogLoggerLevel(lvl)
}
