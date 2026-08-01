// Package config provides configuration loading for the backend service.
package config

import (
	"github.com/rs/zerolog"

	pkgconfig "github.com/radch-ai/ava/pkg/config"
	"github.com/radch-ai/ava/pkg/zitadelvalidate"
)

type Config struct {
	pkgconfig.BaseConfig

	DatabaseURL                       string
	ZitadelDomain                     string
	ZitadelIntrospectorPrivateKeyPath string
	ZitadelWebhookSigningKey          string
}

// Load reads configuration from environment variables.
func Load(logger zerolog.Logger) *Config {
	zitadelDomain := pkgconfig.GetEnvRequired("ZITADEL_DOMAIN", logger)
	if err := zitadelvalidate.ValidateDomain(zitadelDomain); err != nil {
		logger.Fatal().Err(err).Msg("invalid ZITADEL_DOMAIN")
	}

	return &Config{
		BaseConfig:                        pkgconfig.LoadBase("BACKEND", logger),
		DatabaseURL:                       pkgconfig.GetEnvRequired("DATABASE_URL", logger),
		ZitadelDomain:                     zitadelDomain,
		ZitadelIntrospectorPrivateKeyPath: pkgconfig.GetEnvRequired("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", logger),
		ZitadelWebhookSigningKey:          pkgconfig.GetEnvRequired("ZITADEL_WEBHOOK_SIGNING_KEY", logger),
	}
}
