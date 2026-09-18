// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added environment configuration plumbing for the user-service scaffold.
// Author review: PENDING — reviewer to complete

// Package config loads process configuration at the service boundary.
package config

import "os"

// Config contains the environment-provided settings used by user-service.
type Config struct {
	Port               string
	UserDBURL          string
	JWTSecret          string
	JWTAccessTokenTTL  string
	JWTRefreshTokenTTL string
}

// Load reads the service configuration once from the process environment.
func Load() Config {
	return Config{
		Port:               os.Getenv("PORT"),
		UserDBURL:          os.Getenv("USER_DB_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		JWTAccessTokenTTL:  os.Getenv("JWT_ACCESS_TOKEN_TTL"),
		JWTRefreshTokenTTL: os.Getenv("JWT_REFRESH_TOKEN_TTL"),
	}
}
