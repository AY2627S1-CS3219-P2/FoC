// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Tests Load's defaults and its refusal of a malformed JWKS_URL or
//   REDIS_URL, and that the REDIS_URL error never quotes the value.
// Author review: PENDING — <reviewer to complete>

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"JWKS_URL", "REDIS_URL", "PORT", "ORDER_BASE_URL"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8081/.well-known/jwks.json", cfg.JWKSURL)
	assert.Equal(t, "redis://localhost:6379", cfg.RedisURL)
	assert.Equal(t, "8082", cfg.Port)
}

func TestLoadAcceptsTheComposeValues(t *testing.T) {
	t.Setenv("JWKS_URL", "http://user-service:8081/.well-known/jwks.json")
	t.Setenv("REDIS_URL", "rediss://:s3cret@redis:6380/0")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, "http://user-service:8081/.well-known/jwks.json", cfg.JWKSURL)
	assert.Equal(t, "rediss://:s3cret@redis:6380/0", cfg.RedisURL)
}

func TestLoadRefusesAMalformedJWKSURL(t *testing.T) {
	for _, bad := range []string{"user-service:8081/jwks", "ftp://user-service/jwks", "http:///jwks", "://nope", "localhost"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("JWKS_URL", bad)
			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "JWKS_URL")
		})
	}
}

func TestLoadRefusesAMalformedRedisURLWithoutQuotingIt(t *testing.T) {
	for _, bad := range []string{"http://:hunter2@redis:6379", "hunter2@redis:6379", "redis://:hunter2@redis:6379/%zz", "tcp://:hunter2@redis"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("REDIS_URL", bad)
			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "REDIS_URL")
			assert.NotContains(t, err.Error(), "hunter2", "a password in the URL must never reach a log")
		})
	}
}
