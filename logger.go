package main

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func InitLogger(logLevel string) {
	switch logLevel {
	case "none":
		log.Logger = log.Output(zerolog.Nop())
	case "error":
		log.Logger = log.Level(zerolog.ErrorLevel)
	case "info":
		log.Logger = log.Level(zerolog.InfoLevel)
	case "debug":
		log.Logger = log.Level(zerolog.DebugLevel)
	default:
		log.Fatal().Msgf("Unknown log level: %s", logLevel)
	}
}
