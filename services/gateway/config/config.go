// Package config provides configuration loading for the gateway service.
package config

import (
	"os"
	"strconv"

	"github.com/rs/zerolog"

	pkgconfig "github.com/radch-ai/ava/pkg/config"
	"github.com/radch-ai/ava/pkg/zitadelvalidate"
)

type Config struct {
	pkgconfig.BaseConfig

	MaxConns       int
	RedisAddr      string
	RedisPassword  string
	BifrostBaseURL string
	DatabaseURL    string
	ZitadelDomain  string
	ZitadelKeyPath string
}

// Load reads configuration from environment variables.
func Load(logger zerolog.Logger) *Config {
	zitadelDomain := pkgconfig.GetEnvRequired("ZITADEL_DOMAIN", logger)
	if err := zitadelvalidate.ValidateDomain(zitadelDomain); err != nil {
		logger.Fatal().Err(err).Msg("invalid ZITADEL_DOMAIN")
	}

	return &Config{
		BaseConfig:     pkgconfig.LoadBase("GATEWAY"),
		MaxConns:       loadMaxConns(logger),
		RedisAddr:      pkgconfig.GetEnvRequired("REDIS_ADDR", logger),
		RedisPassword:  pkgconfig.GetEnvRequired("REDIS_PASSWORD", logger),
		BifrostBaseURL: pkgconfig.GetEnvRequired("BIFROST_BASEURL", logger),
		DatabaseURL:    pkgconfig.GetEnvRequired("DATABASE_URL", logger),
		ZitadelDomain:  zitadelDomain,
		ZitadelKeyPath: pkgconfig.GetEnvRequired("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", logger),
	}
}

// loadMaxConns reads GATEWAY_MAX_CONNS, defaulting to 10000.
func loadMaxConns(logger zerolog.Logger) int {
	maxConns := 10000
	if maxConnsStr := os.Getenv("GATEWAY_MAX_CONNS"); maxConnsStr != "" {
		var err error
		maxConns, err = strconv.Atoi(maxConnsStr)
		if err != nil {
			logger.Fatal().Str("value", maxConnsStr).Err(err).Msg("invalid GATEWAY_MAX_CONNS")
		}
		if maxConns <= 0 {
			logger.Fatal().Str("value", maxConnsStr).Msg("GATEWAY_MAX_CONNS must be a positive integer")
		}
	}
	return maxConns
}
