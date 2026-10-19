// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented strict configuration loader and validation for credit-service per sourceOfTruth.md Phase 12.
// Author review: PENDING - reviewer to complete

package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config holds runtime configuration read from environment variables.
type Config struct {
	Port                    string
	DatabaseURL             string
	JWKSURL                 string
	RabbitMQURL             string
	RunMigrations           bool
	MigrationsSourceURL     string
	PayoutRetryInterval     time.Duration
	RefundRetryInterval     time.Duration
	PayoutAlertAfter        time.Duration
	RefundAlertAfter        time.Duration
	ReconciliationInterval  time.Duration
	PayoutBatchSize         int
	RefundBatchSize         int
	ReconciliationBatchSize int
	WorkerConcurrency       int
	PayoutWorkerConcurrency int
	RefundWorkerConcurrency int
}

// Load reads credit-service configuration from environment variables and applies defaults.
func Load() (Config, error) {
	cfg := Config{
		Port:                    envOrDefault("PORT", "8084"),
		DatabaseURL:             os.Getenv("CREDIT_DB_URL"),
		JWKSURL:                 os.Getenv("JWKS_URL"),
		RabbitMQURL:             os.Getenv("RABBITMQ_URL"),
		RunMigrations:           envOrDefaultBool("RUN_MIGRATIONS", true),
		MigrationsSourceURL:     envOrDefault("MIGRATIONS_SOURCE_URL", "file://migrations"),
		PayoutRetryInterval:     envOrDefaultDuration("PAYOUT_RETRY_INTERVAL", 1*time.Minute),
		RefundRetryInterval:     envOrDefaultDuration("REFUND_RETRY_INTERVAL", 1*time.Minute),
		PayoutAlertAfter:        envOrDefaultDuration("PAYOUT_ALERT_AFTER", 15*time.Minute),
		RefundAlertAfter:        envOrDefaultDuration("REFUND_ALERT_AFTER", 15*time.Minute),
		ReconciliationInterval:  envOrDefaultDuration("RECONCILIATION_INTERVAL", 5*time.Minute),
		PayoutBatchSize:         envOrDefaultInt("PAYOUT_BATCH_SIZE", 50),
		RefundBatchSize:         envOrDefaultInt("REFUND_BATCH_SIZE", 50),
		ReconciliationBatchSize: envOrDefaultInt("RECONCILIATION_BATCH_SIZE", 500),
		WorkerConcurrency:       envOrDefaultInt("WORKER_CONCURRENCY", 2),
	}

	cfg.PayoutWorkerConcurrency = envOrDefaultInt("PAYOUT_WORKER_CONCURRENCY", cfg.WorkerConcurrency)
	cfg.RefundWorkerConcurrency = envOrDefaultInt("REFUND_WORKER_CONCURRENCY", cfg.WorkerConcurrency)

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Addr returns the HTTP server listen address.
func (cfg Config) Addr() string {
	return ":" + cfg.Port
}

// Validate checks required environment variables and configuration invariants.
func (cfg Config) Validate() error {
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("CREDIT_DB_URL is required"))
	}
	if cfg.JWKSURL == "" {
		errs = append(errs, errors.New("JWKS_URL is required"))
	}
	if cfg.RabbitMQURL == "" {
		errs = append(errs, errors.New("RABBITMQ_URL is required"))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func envOrDefault(key string, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

func envOrDefaultBool(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
}

func envOrDefaultDuration(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	d, err := time.ParseDuration(val)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func envOrDefaultInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
