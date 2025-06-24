package main

import (
	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func InitLogger() {
	switch config.Config.LogLevelFlag {
	case "none":
		log.Logger = log.Output(zerolog.Nop())
	case "error":
		log.Logger = log.Level(zerolog.ErrorLevel)
	case "info":
		log.Logger = log.Level(zerolog.InfoLevel)
	case "debug":
		log.Logger = log.Level(zerolog.DebugLevel)
	default:
		log.Fatal().Msgf("Unknown log level: %s", config.Config.LogLevelFlag)
	}
}
