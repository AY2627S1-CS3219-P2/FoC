// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added focused tests for environment configuration loading.
// Author review: COMPLETED BY ZI YANG

package config

import "testing"

func TestLoadReadsServiceEnvironment(t *testing.T) {
	t.Setenv("PORT", "8081")
	t.Setenv("USER_DB_URL", "postgres://user-db")
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	t.Setenv("JWT_KEYSET_PATH", "/run/secrets/keys.json")
	t.Setenv("JWT_ACCESS_TOKEN_TTL", "15m")
	t.Setenv("JWT_REFRESH_TOKEN_TTL", "7d")

	got := Load()

	want := Config{
		Port:               "8081",
		UserDBURL:          "postgres://user-db",
		RedisURL:           "redis://redis:6379/0",
		JWTKeySetPath:      "/run/secrets/keys.json",
		JWTAccessTokenTTL:  "15m",
		JWTRefreshTokenTTL: "7d",
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadUsesEmptyValuesWhenEnvironmentIsUnset(t *testing.T) {
	for _, name := range []string{
		"PORT",
		"USER_DB_URL",
		"REDIS_URL",
		"JWT_KEYSET_PATH",
		"JWT_ACCESS_TOKEN_TTL",
		"JWT_REFRESH_TOKEN_TTL",
	} {
		t.Setenv(name, "")
	}

	if got := Load(); got != (Config{}) {
		t.Fatalf("Load() = %#v, want empty config", got)
	}
}
