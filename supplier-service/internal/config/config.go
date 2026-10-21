package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	SeedCSVPath string

	// AI-generated (edited by James Teo).
	// OrderBaseURL is order-service, asked before every delete whether an
	// unfinished errand still references the supplier (FR F2.2.3).
	OrderBaseURL string

	// AI Assistance Disclosure:
	// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
	// Scope: JWKSURL and RedisURL, for verifying admin tokens and checking
	//   their revocation (D-041, D-054); Load now validates them.
	// Author review: PENDING — <reviewer to complete>

	// JWKSURL is where user-service publishes its signing keys (D-023).
	JWKSURL string
	// RedisURL is the Redis user-service writes revocation keys to (D-054).
	RedisURL string
}

// Load reads the configuration from the environment. A malformed JWKS_URL or
// REDIS_URL is an error; the REDIS_URL one never quotes the value, which can
// carry a password.
func Load() (Config, error) {
	cfg := Config{
		Port:        getenv("PORT", "8082"),
		DatabaseURL: getenv("SUPPLIER_DB_URL", "postgres://postgres:postgres@localhost:5432/supplier?sslmode=disable"),
		SeedCSVPath: getenv("SEED_CSV_PATH", "../data/csv/supplier-seed-data.csv"),

		// AI-generated (edited by James Teo).
		OrderBaseURL: getenv("ORDER_BASE_URL", "http://localhost:8083"),

		JWKSURL:  getenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		RedisURL: getenv("REDIS_URL", "redis://localhost:6379"),
	}
	if u, err := url.Parse(cfg.JWKSURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, fmt.Errorf("JWKS_URL must be an http:// or https:// URL with a host, got %q", cfg.JWKSURL)
	}
	if u, err := url.Parse(cfg.RedisURL); err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") {
		return Config{}, errors.New("REDIS_URL must be a redis:// or rediss:// URL with reserved characters in the user and password percent-encoded")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
