package utils

import (
	"os"

	"github.com/rs/zerolog"
)

// SetupLogger configures the global zerolog level from <prefix>_LOG_LEVEL and
// returns a logger tagged with the service name.
func SetupLogger(service, prefix string) zerolog.Logger {
	level := GetEnv(prefix+"_LOG_LEVEL", "info")

	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		// Configured logger isn't constructed yet; use a throwaway one to report.
		logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		logger.Fatal().
			Str("level", level).
			Err(err).
			Msg("invalid log level")
	}
	zerolog.SetGlobalLevel(logLevel)

	return zerolog.New(os.Stderr).With().Timestamp().Str("service", service).Logger()
}
