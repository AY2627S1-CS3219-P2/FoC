// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Unit tests for config.Load.
// Author review: nigeltzy - Checked that the unit test implementation seems correct and as expected according to requirements.

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "")
	t.Setenv("ORDER_CONFIRMATION_WINDOW", "")
	t.Setenv("NOTIFICATION_DB_URL", "postgres://db")
	t.Setenv("RABBITMQ_URL", "amqp://broker")
	t.Setenv("JWKS_URL", "http://keys")
	t.Setenv("REDIS_URL", "redis://redis")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "8086", cfg.Port)
	assert.Equal(t, 24*time.Hour, cfg.ConfirmWindow)
	assert.Equal(t, "postgres://db", cfg.DatabaseURL)
}

func TestLoadReadsOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "9000")
	t.Setenv("ORDER_CONFIRMATION_WINDOW", "45s")
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "9000", cfg.Port)
	assert.Equal(t, 45*time.Second, cfg.ConfirmWindow)
}

func TestLoadNamesWhatIsMissing(t *testing.T) {
	for _, name := range []string{"NOTIFICATION_DB_URL", "RABBITMQ_URL", "JWKS_URL", "REDIS_URL"} {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(name, "")
			_, err := Load()
			require.ErrorContains(t, err, name)
		})
	}
}

func TestLoadRefusesABadWindow(t *testing.T) {
	for _, value := range []string{"abc", "0", "-1h"} {
		t.Run(value, func(t *testing.T) {
			setRequired(t)
			t.Setenv("ORDER_CONFIRMATION_WINDOW", value)
			_, err := Load()
			require.ErrorContains(t, err, "ORDER_CONFIRMATION_WINDOW")
		})
	}
}

// AI-generated (edited by nigeltzy).
// TestLoadRefusesABrokerURLItCannotParse: the error is fixed text, because the
// URL carries the broker's password and net/url's own error quotes it.
func TestLoadRefusesABrokerURLItCannotParse(t *testing.T) {
	for _, value := range []string{
		"amqp://foc:s3cr/et@broker:5672/foc",
		"amqp://foc:s3cr?et@broker:5672/foc",
		"http://foc:s3cret@broker:5672/foc",
		"foc:s3cret@broker",
	} {
		t.Run(value, func(t *testing.T) {
			setRequired(t)
			t.Setenv("RABBITMQ_URL", value)
			_, err := Load()
			require.ErrorContains(t, err, "RABBITMQ_URL")
			assert.NotContains(t, err.Error(), "s3cr")
		})
	}
}

// AI-generated (edited by nigeltzy).
func TestLoadAcceptsAnEncodedBrokerPassword(t *testing.T) {
	setRequired(t)
	t.Setenv("RABBITMQ_URL", "amqps://foc:s3cr%2Fet@broker:5671/foc")
	_, err := Load()
	require.NoError(t, err)
}
