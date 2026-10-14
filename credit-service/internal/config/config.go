// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-09-18
// Scope: Generated initial environment configuration loader for credit-service scaffold.
// Author review: PENDING - reviewer to complete

package config

import "os"

// Config holds runtime configuration read from environment variables.
type Config struct {
	Port        string
	DatabaseURL string
	JWKSURL     string
}

// Load reads credit-service configuration from environment variables.
func Load() Config {
	return Config{
		Port:        envOrDefault("PORT", "8084"),
		DatabaseURL: os.Getenv("CREDIT_DB_URL"),
		JWKSURL:     os.Getenv("JWKS_URL"),
	}
}

// Addr returns the HTTP server listen address.
func (cfg Config) Addr() string {
	return ":" + cfg.Port
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
