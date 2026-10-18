// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Unit tests for Load: D-049's defaults for T, K and C, the port and
//   callee URLs from root AGENTS.md §3, overrides, and malformed values.
// Author review: James Teo — reviewed and verified.

package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/config"
)

// keys are every variable Load reads.
func keys() []string {
	return []string{
		"PORT", "ORDER_DB_URL", "SUPPLIER_BASE_URL", "CREDIT_BASE_URL", "RABBITMQ_URL",
		"JWKS_URL", "REDIS_URL", "ORDER_EXPIRY_PERIOD", "ORDER_MAX_ACTIVE_ASSIGNMENTS", "ORDER_CONFIRMATION_WINDOW",
	}
}

// setEnv blanks every variable Load reads, then sets vars. Load treats an
// empty value as unset, and t.Setenv restores each one when the test ends,
// so no test sees the developer's environment or another test's.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range keys() {
		t.Setenv(k, vars[k])
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "8083", cfg.Port, "order-service's port (root AGENTS.md §3)")
	assert.Equal(t, 2*time.Hour, cfg.ExpiryPeriod, "T")
	assert.Equal(t, 2, cfg.MaxActive, "K")
	assert.Equal(t, 24*time.Hour, cfg.ConfirmWindow, "C")
	assert.Equal(t, "http://localhost:8082", cfg.SupplierBaseURL, "supplier-service's port (root AGENTS.md §3)")
	assert.Equal(t, "http://localhost:8084", cfg.CreditBaseURL, "credit-service's port (root AGENTS.md §3)")
	assert.NotEmpty(t, cfg.DatabaseURL)
	assert.NotEmpty(t, cfg.RabbitMQURL)
	assert.Equal(t, "http://localhost:8081/.well-known/jwks.json", cfg.JWKSURL, "user-service's keys")
	assert.Equal(t, "redis://localhost:6379", cfg.RedisURL, "the shared Redis")
}

func TestLoadOverrides(t *testing.T) {
	// D-049: each of T, K and C can be overridden; the e2e suite uses seconds.
	setEnv(t, map[string]string{
		"PORT":                         "9083",
		"ORDER_DB_URL":                 "postgres://order-db:5432/orders",
		"SUPPLIER_BASE_URL":            "http://supplier-service:8082",
		"CREDIT_BASE_URL":              "http://credit-service:8084",
		"RABBITMQ_URL":                 "amqp://rabbitmq:5672/foc",
		"JWKS_URL":                     "http://user-service:8081/.well-known/jwks.json",
		"REDIS_URL":                    "redis://redis:6379",
		"ORDER_EXPIRY_PERIOD":          "30s",
		"ORDER_MAX_ACTIVE_ASSIGNMENTS": "5",
		"ORDER_CONFIRMATION_WINDOW":    "45s",
	})

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, config.Config{
		Port:            "9083",
		DatabaseURL:     "postgres://order-db:5432/orders",
		SupplierBaseURL: "http://supplier-service:8082",
		CreditBaseURL:   "http://credit-service:8084",
		RabbitMQURL:     "amqp://rabbitmq:5672/foc",
		JWKSURL:         "http://user-service:8081/.well-known/jwks.json",
		RedisURL:        "redis://redis:6379",
		ExpiryPeriod:    30 * time.Second,
		MaxActive:       5,
		ConfirmWindow:   45 * time.Second,
	}, cfg)
}

func TestLoadRejectsMalformedValues(t *testing.T) {
	cases := []struct {
		key, value string
	}{
		{"ORDER_EXPIRY_PERIOD", "abc"},
		{"ORDER_EXPIRY_PERIOD", "2"},
		{"ORDER_EXPIRY_PERIOD", "0s"},
		{"ORDER_EXPIRY_PERIOD", "-5m"},
		{"ORDER_CONFIRMATION_WINDOW", "abc"},
		{"ORDER_CONFIRMATION_WINDOW", "0"},
		{"ORDER_CONFIRMATION_WINDOW", "-1h"},
		{"ORDER_MAX_ACTIVE_ASSIGNMENTS", "0"},
		{"ORDER_MAX_ACTIVE_ASSIGNMENTS", "-1"},
		{"ORDER_MAX_ACTIVE_ASSIGNMENTS", "abc"},
		{"ORDER_MAX_ACTIVE_ASSIGNMENTS", "1.5"},
		{"JWKS_URL", "user-service:8081/.well-known/jwks.json"},
		{"JWKS_URL", "ftp://user-service/keys"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setEnv(t, map[string]string{tc.key: tc.value})

			cfg, err := config.Load()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.key, "the error names the variable")
			assert.Contains(t, err.Error(), tc.value, "and the value it could not use")
			assert.Equal(t, config.Config{}, cfg)
		})
	}

	// RABBITMQ_URL carries the broker password, so unlike the values above
	// its error names the variable but quotes none of the value. secrets are
	// fragments of the password as a leaking error would print them: net/url
	// quotes the whole URL, and its inner error can quote part of the
	// password on its own (":pa", "%t-"). "pa" and "ss" alone are not checked,
	// because the message's own word "password" contains both.
	brokerURLs := []struct {
		name, value string
		secrets     []string
	}{
		{"an unencoded percent sign in the password", "amqp://foc:s3cr%t-pw@rabbitmq:5672/foc", []string{"s3cr", "%t-", "-pw"}},
		{"an unencoded slash in the password", "amqp://foc:pa/ss@rabbitmq:5672/foc", []string{"pa/ss", ":pa"}},
		{"a non-numeric port", "amqp://foc:hunter2@rabbitmq:56x2/foc", []string{"hunter2"}},
		{"an http scheme", "http://rabbitmq:5672", nil},
	}
	// REDIS_URL can carry a password too, so it is held to the same rule.
	brokerURLs = append(brokerURLs,
		struct {
			name, value string
			secrets     []string
		}{"a Redis URL with an unencoded percent sign", "redis://:s3cr%t-pw@redis:6379", []string{"s3cr", "%t-", "-pw"}},
	)
	for _, tc := range brokerURLs {
		key := "RABBITMQ_URL"
		if strings.HasPrefix(tc.value, "redis") {
			key = "REDIS_URL"
		}
		t.Run(key+" with "+tc.name, func(t *testing.T) {
			setEnv(t, map[string]string{key: tc.value})

			cfg, err := config.Load()

			require.Error(t, err)
			assert.Contains(t, err.Error(), key, "the error names the variable")
			assert.NotContains(t, err.Error(), tc.value, "but does not echo the value")
			for _, s := range tc.secrets {
				assert.NotContains(t, err.Error(), s, "or any part of the password")
			}
			assert.Equal(t, config.Config{}, cfg)
		})
	}
}
