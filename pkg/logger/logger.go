package logger

import (
	"github.com/ditto-assistant/agentflow/cfg"
	"github.com/ditto-assistant/agentflow/pkg/assert"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func Setup() {
	lvl, err := zerolog.ParseLevel(cfg.LogLevel)
	assert.NoError(err)
	zerolog.SetGlobalLevel(lvl)
	log.Logger = zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	log.Trace().Msg("Logger setup")
}

func SetupLevel(lvl zerolog.Level) {
	zerolog.SetGlobalLevel(lvl)
	log.Logger = zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	log.Trace().Msg("Logger setup")
}
