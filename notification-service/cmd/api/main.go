// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Wires notification-service: database, migrations, the consumer on its
//   own queue (D-140), token checks and the HTTP server. Follows
//   order-service/cmd/api/main.go.
// Author review: nigeltzy - Checked that the implementation is as expected and seems to be correct.

// Command api runs notification-service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"foc/notification-service/internal/auth"
	"foc/notification-service/internal/config"
	"foc/notification-service/internal/consumer"
	"foc/notification-service/internal/httpapi"
	"foc/notification-service/internal/notification"
	"foc/notification-service/internal/repository"
)

const (
	// dbReadyTimeout bounds how long startup waits for PostgreSQL (root
	// AGENTS.md §5: never assume startup order).
	dbReadyTimeout = 60 * time.Second
	// shutdownTimeout bounds graceful shutdown; compose.yaml's stop_grace_period
	// for this service stays above it.
	shutdownTimeout = 20 * time.Second
	jwksTimeout     = 5 * time.Second
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("notification-service stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("configure database pool: %w", err)
	}
	defer pool.Close()
	if err := waitForDatabase(ctx, pool, log); err != nil {
		return err
	}
	if err := repository.Migrate(cfg.DatabaseURL, migrationsDir()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	svc := notification.NewService(repository.NewPostgres(pool), cfg.ConfirmWindow)

	// Keys are fetched on first use and go-redis connects lazily, so neither
	// user-service nor Redis has to be up for this service to start.
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return errors.New("REDIS_URL could not be parsed")
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()
	verifier := auth.NewVerifier(cfg.JWKSURL, &http.Client{Timeout: jwksTimeout})

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		consumer.New(cfg.RabbitMQURL, svc, log).Run(ctx)
	}()

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewRouter(svc, pool, verifier, auth.NewRevocations(rdb), log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		// AI-generated (edited by nigeltzy).
		// net/http's own errors, such as a panic outside the router's
		// recovery, would otherwise go to stderr as plain text (D-135).
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Info("notification-service listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil {
			stop()
			return fmt.Errorf("http server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown", "err", err)
	}
	select {
	case <-consumerDone:
	case <-shutdownCtx.Done():
	}
	log.Info("notification-service stopped")
	return nil
}

// waitForDatabase pings until PostgreSQL answers or dbReadyTimeout passes.
func waitForDatabase(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	deadline := time.Now().Add(dbReadyTimeout)
	backoff := 500 * time.Millisecond
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database not reachable after %s: %w", dbReadyTimeout, err)
		}
		log.Warn("waiting for database", "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

// migrationsDir finds ./migrations for a host run, or the image's copy.
func migrationsDir() string {
	dir, err := filepath.Abs("migrations")
	if err == nil {
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir
		}
	}
	return "/app/migrations"
}
