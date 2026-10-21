// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Composition root, first-error coordinator, and graceful shutdown wiring per sourceOfTruth.md Phase 12.
// Author review: PENDING - reviewer to complete

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/config"
	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/httpapi"
	"foc/credit-service/internal/messaging"
	"foc/credit-service/internal/repository"
	"foc/credit-service/internal/worker"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration initialization failed", "error", err)
		os.Exit(1)
	}

	if err := run(context.Background(), cfg, logger); err != nil {
		logger.Error("credit service stopped with fatal error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	// Database Connection Pool
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	logger.Info("connected to PostgreSQL pool")

	// Database Migrations & Schema Version Verification
	var schemaReady atomic.Bool
	if cfg.RunMigrations {
		if err := repository.Apply(cfg.DatabaseURL, cfg.MigrationsSourceURL); err != nil {
			return fmt.Errorf("apply database migrations: %w", err)
		}
		schemaReady.Store(true)
		logger.Info("database migrations applied successfully")
	} else {
		version, dirty, err := repository.GetSchemaVersion(cfg.DatabaseURL, cfg.MigrationsSourceURL)
		if err != nil {
			return fmt.Errorf("inspect database schema version: %w", err)
		}
		if dirty {
			return errors.New("database schema is in a dirty state")
		}
		if version > repository.ExpectedSchemaVersion {
			return fmt.Errorf("incompatible schema version %d exceeds supported %d", version, repository.ExpectedSchemaVersion)
		}
		if version < repository.ExpectedSchemaVersion {
			logger.Warn("database schema is older than expected, readiness will fail until migrated",
				"currentVersion", version,
				"expectedVersion", repository.ExpectedSchemaVersion,
			)
			schemaReady.Store(false)
		} else {
			schemaReady.Store(true)
			logger.Info("database schema version verified", "version", version)
		}
	}

	// Repository and Domain Service
	creditRepo := repository.NewPostgresRepository(pool)
	creditService := credit.NewService(creditRepo)

	// Redis Client & Revocation Checker (D-014, D-054, D-056)
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer func() {
		if err := rdb.Close(); err != nil {
			logger.Warn("close Redis client", "err", err)
		}
	}()
	pingRedis(ctx, rdb, logger)
	revocations := auth.NewRevocations(rdb)

	// Auth Verifier (D-023, D-056)
	verifier := auth.NewVerifier(cfg.JWKSURL, &http.Client{Timeout: 5 * time.Second})

	// Messaging Handlers and Supervisor
	regHandler := messaging.NewRegistrationHandler(creditService, messaging.WithLogger(logger.With("component", "registration_handler")))
	lifeHandler := messaging.NewLifecycleHandler(creditService, logger.With("component", "lifecycle_handler"))
	supervisor := messaging.NewSupervisor(
		cfg.RabbitMQURL,
		regHandler,
		lifeHandler,
		cfg.WorkerConcurrency,
		logger.With("component", "messaging_supervisor"),
	)

	// Background Workers
	payoutWorker := worker.NewPendingPayoutWorker(
		creditRepo,
		cfg.PayoutRetryInterval,
		cfg.PayoutAlertAfter,
		cfg.PayoutBatchSize,
		logger.With("component", "pending_payout_worker"),
	)
	refundWorker := worker.NewPendingRefundWorker(
		creditRepo,
		cfg.RefundRetryInterval,
		cfg.RefundAlertAfter,
		cfg.RefundBatchSize,
		logger.With("component", "pending_refund_worker"),
	)
	reconciliationWorker := worker.NewReconciliationWorker(
		creditRepo,
		cfg.ReconciliationInterval,
		cfg.ReconciliationBatchSize,
		logger.With("component", "reconciliation_worker"),
	)

	// Readiness Health Check
	healthCheck := func(checkCtx context.Context) error {
		if err := pool.Ping(checkCtx); err != nil {
			return errors.New("database unavailable")
		}
		if !schemaReady.Load() {
			return errors.New("schema migration unready")
		}
		if !supervisor.IsReady() {
			return errors.New("rabbitmq supervisor unready")
		}
		return nil
	}

	// HTTP Server with Chi Router
	router := httpapi.NewRouter(healthCheck, verifier, revocations, creditService, logger)
	server := newHTTPServer(cfg.Addr(), router)

	// First-error Coordination and Intake Context
	intakeCtx, cancelIntake := context.WithCancel(ctx)
	defer cancelIntake()

	coord := newCoordinator(cancelIntake, logger)

	coord.wg.Add(1)
	go func() {
		defer coord.wg.Done()
		logger.Info("starting credit service HTTP server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			coord.report("http_server", fmt.Errorf("serve HTTP: %w", err))
		}
	}()

	coord.wg.Add(1)
	go func() {
		defer coord.wg.Done()
		logger.Info("starting messaging supervisor")
		if err := supervisor.Run(intakeCtx); err != nil && !errors.Is(err, context.Canceled) {
			coord.report("messaging_supervisor", fmt.Errorf("messaging supervisor: %w", err))
		}
	}()

	coord.wg.Add(1)
	go func() {
		defer coord.wg.Done()
		logger.Info("starting pending payout worker")
		if err := payoutWorker.Run(intakeCtx); err != nil && !errors.Is(err, context.Canceled) {
			coord.report("pending_payout_worker", fmt.Errorf("pending payout worker: %w", err))
		}
	}()

	coord.wg.Add(1)
	go func() {
		defer coord.wg.Done()
		logger.Info("starting pending refund worker")
		if err := refundWorker.Run(intakeCtx); err != nil && !errors.Is(err, context.Canceled) {
			coord.report("pending_refund_worker", fmt.Errorf("pending refund worker: %w", err))
		}
	}()

	coord.wg.Add(1)
	go func() {
		defer coord.wg.Done()
		logger.Info("starting reconciliation worker")
		if err := reconciliationWorker.Run(intakeCtx); err != nil && !errors.Is(err, context.Canceled) {
			coord.report("reconciliation_worker", fmt.Errorf("reconciliation worker: %w", err))
		}
	}()

	// Signal and Lifecycle Management
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
		cancelIntake()
	case <-intakeCtx.Done():
		// Coordinator cancelled intake or parent context cancelled
	}

	// Graceful Drain Context
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDrain()

	if err := server.Shutdown(drainCtx); err != nil {
		logger.Error("error during HTTP server shutdown", "error", err)
	}

	doneCh := make(chan struct{})
	go func() {
		coord.wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		logger.Info("all background components stopped cleanly")
	case <-drainCtx.Done():
		logger.Warn("graceful drain timeout exceeded, forcing termination")
	}

	return coord.firstError()
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func pingRedis(ctx context.Context, rdb *redis.Client, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("Redis ping at startup failed; while unreachable, revocation checks are skipped, "+
			"and while answering with errors, token requests are refused with 503", "err", err)
	}
}
