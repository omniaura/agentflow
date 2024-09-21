package logger

import (
	"log/slog"

	"github.com/ditto-assistant/agentflow/cfg"
	"github.com/ditto-assistant/agentflow/pkg/assert"
)

func Setup() {
	var lvl slog.Level
	err := lvl.UnmarshalText([]byte(cfg.LogLevel))
	assert.NoError(err)
	SetupLevel(lvl)
}

func SetupLevel(lvl slog.Level) {
	slog.SetLogLoggerLevel(lvl)
}
