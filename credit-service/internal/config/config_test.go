// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for configuration parsing and validation per sourceOfTruth.md Phase 12.
// Author review: PENDING - reviewer to complete

package config_test

import (
	"testing"
	"time"

	"foc/credit-service/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("CREDIT_DB_URL", "postgres://test:test@localhost:5432/credit")
	t.Setenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json")
	t.Setenv("RABBITMQ_URL", "amqp://test:test@localhost:5672/test")
	t.Setenv("RUN_MIGRATIONS", "")
	t.Setenv("PAYOUT_RETRY_INTERVAL", "")
	t.Setenv("REFUND_RETRY_INTERVAL", "")
	t.Setenv("PAYOUT_ALERT_AFTER", "")
	t.Setenv("REFUND_ALERT_AFTER", "")
	t.Setenv("RECONCILIATION_INTERVAL", "")
	t.Setenv("PAYOUT_BATCH_SIZE", "")
	t.Setenv("REFUND_BATCH_SIZE", "")
	t.Setenv("RECONCILIATION_BATCH_SIZE", "")
	t.Setenv("WORKER_CONCURRENCY", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "8084" {
		t.Errorf("Port = %s, want 8084", cfg.Port)
	}
	if cfg.Addr() != ":8084" {
		t.Errorf("Addr() = %s, want :8084", cfg.Addr())
	}
	if !cfg.RunMigrations {
		t.Errorf("RunMigrations = false, want true")
	}
	if cfg.PayoutRetryInterval != 1*time.Minute {
		t.Errorf("PayoutRetryInterval = %v, want 1m", cfg.PayoutRetryInterval)
	}
	if cfg.RefundRetryInterval != 1*time.Minute {
		t.Errorf("RefundRetryInterval = %v, want 1m", cfg.RefundRetryInterval)
	}
	if cfg.PayoutAlertAfter != 15*time.Minute {
		t.Errorf("PayoutAlertAfter = %v, want 15m", cfg.PayoutAlertAfter)
	}
	if cfg.RefundAlertAfter != 15*time.Minute {
		t.Errorf("RefundAlertAfter = %v, want 15m", cfg.RefundAlertAfter)
	}
	if cfg.ReconciliationInterval != 5*time.Minute {
		t.Errorf("ReconciliationInterval = %v, want 5m", cfg.ReconciliationInterval)
	}
	if cfg.PayoutBatchSize != 50 {
		t.Errorf("PayoutBatchSize = %d, want 50", cfg.PayoutBatchSize)
	}
	if cfg.RefundBatchSize != 50 {
		t.Errorf("RefundBatchSize = %d, want 50", cfg.RefundBatchSize)
	}
	if cfg.ReconciliationBatchSize != 500 {
		t.Errorf("ReconciliationBatchSize = %d, want 500", cfg.ReconciliationBatchSize)
	}
	if cfg.WorkerConcurrency != 2 {
		t.Errorf("WorkerConcurrency = %d, want 2", cfg.WorkerConcurrency)
	}
	if cfg.PayoutWorkerConcurrency != 2 {
		t.Errorf("PayoutWorkerConcurrency = %d, want 2", cfg.PayoutWorkerConcurrency)
	}
	if cfg.RefundWorkerConcurrency != 2 {
		t.Errorf("RefundWorkerConcurrency = %d, want 2", cfg.RefundWorkerConcurrency)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	t.Run("missing all required", func(t *testing.T) {
		t.Setenv("CREDIT_DB_URL", "")
		t.Setenv("JWKS_URL", "")
		t.Setenv("RABBITMQ_URL", "")

		_, err := config.Load()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("missing db url", func(t *testing.T) {
		t.Setenv("CREDIT_DB_URL", "")
		t.Setenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json")
		t.Setenv("RABBITMQ_URL", "amqp://test:test@localhost:5672/test")

		_, err := config.Load()
		if err == nil {
			t.Fatal("expected error for missing CREDIT_DB_URL, got nil")
		}
	})

	t.Run("missing jwks url", func(t *testing.T) {
		t.Setenv("CREDIT_DB_URL", "postgres://test:test@localhost:5432/credit")
		t.Setenv("JWKS_URL", "")
		t.Setenv("RABBITMQ_URL", "amqp://test:test@localhost:5672/test")

		_, err := config.Load()
		if err == nil {
			t.Fatal("expected error for missing JWKS_URL, got nil")
		}
	})

	t.Run("missing rabbitmq url", func(t *testing.T) {
		t.Setenv("CREDIT_DB_URL", "postgres://test:test@localhost:5432/credit")
		t.Setenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json")
		t.Setenv("RABBITMQ_URL", "")

		_, err := config.Load()
		if err == nil {
			t.Fatal("expected error for missing RABBITMQ_URL, got nil")
		}
	})
}

func TestLoad_CustomOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("CREDIT_DB_URL", "postgres://test:test@localhost:5432/credit")
	t.Setenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json")
	t.Setenv("RABBITMQ_URL", "amqp://test:test@localhost:5672/test")
	t.Setenv("RUN_MIGRATIONS", "false")
	t.Setenv("PAYOUT_RETRY_INTERVAL", "45s")
	t.Setenv("REFUND_RETRY_INTERVAL", "30s")
	t.Setenv("PAYOUT_ALERT_AFTER", "10m")
	t.Setenv("REFUND_ALERT_AFTER", "8m")
	t.Setenv("RECONCILIATION_INTERVAL", "2m")
	t.Setenv("PAYOUT_BATCH_SIZE", "100")
	t.Setenv("REFUND_BATCH_SIZE", "80")
	t.Setenv("RECONCILIATION_BATCH_SIZE", "1000")
	t.Setenv("WORKER_CONCURRENCY", "4")
	t.Setenv("PAYOUT_WORKER_CONCURRENCY", "6")
	t.Setenv("REFUND_WORKER_CONCURRENCY", "8")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("Port = %s, want 9090", cfg.Port)
	}
	if cfg.RunMigrations {
		t.Errorf("RunMigrations = true, want false")
	}
	if cfg.PayoutRetryInterval != 45*time.Second {
		t.Errorf("PayoutRetryInterval = %v, want 45s", cfg.PayoutRetryInterval)
	}
	if cfg.RefundRetryInterval != 30*time.Second {
		t.Errorf("RefundRetryInterval = %v, want 30s", cfg.RefundRetryInterval)
	}
	if cfg.PayoutAlertAfter != 10*time.Minute {
		t.Errorf("PayoutAlertAfter = %v, want 10m", cfg.PayoutAlertAfter)
	}
	if cfg.RefundAlertAfter != 8*time.Minute {
		t.Errorf("RefundAlertAfter = %v, want 8m", cfg.RefundAlertAfter)
	}
	if cfg.ReconciliationInterval != 2*time.Minute {
		t.Errorf("ReconciliationInterval = %v, want 2m", cfg.ReconciliationInterval)
	}
	if cfg.PayoutBatchSize != 100 {
		t.Errorf("PayoutBatchSize = %d, want 100", cfg.PayoutBatchSize)
	}
	if cfg.RefundBatchSize != 80 {
		t.Errorf("RefundBatchSize = %d, want 80", cfg.RefundBatchSize)
	}
	if cfg.ReconciliationBatchSize != 1000 {
		t.Errorf("ReconciliationBatchSize = %d, want 1000", cfg.ReconciliationBatchSize)
	}
	if cfg.WorkerConcurrency != 4 {
		t.Errorf("WorkerConcurrency = %d, want 4", cfg.WorkerConcurrency)
	}
	if cfg.PayoutWorkerConcurrency != 6 {
		t.Errorf("PayoutWorkerConcurrency = %d, want 6", cfg.PayoutWorkerConcurrency)
	}
	if cfg.RefundWorkerConcurrency != 8 {
		t.Errorf("RefundWorkerConcurrency = %d, want 8", cfg.RefundWorkerConcurrency)
	}
}
