// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Wiring only: config, database (waits for readiness), migrations, the
//   adapters, the service, the sweeper (D-049), the outbox relay (D-051) and the
//   HTTP server, with graceful shutdown.
// Author review: James Teo — reviewed and verified.

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

	"foc/order-service/internal/auth"
	"foc/order-service/internal/config"
	"foc/order-service/internal/creditclient"
	"foc/order-service/internal/httpapi"
	"foc/order-service/internal/order"
	"foc/order-service/internal/relay"
	"foc/order-service/internal/repository"
	"foc/order-service/internal/supplierclient"
	"foc/order-service/internal/sweeper"
)

// The exchange rabbitmq/definitions.json declares; D-051 publishes only to it.
const eventsExchange = "foc.events"

// sweepInterval is D-049's "every 5 seconds".
const sweepInterval = 5 * time.Second

// dbReadyTimeout bounds how long startup waits for PostgreSQL. Compose starts
// order-service only once order-db is healthy, but a plain `go run` or a
// restarted database must not crash it (root AGENTS.md §5: never assume
// startup order).
const dbReadyTimeout = 60 * time.Second

// AI-generated (edited by James Teo).
// redisPingTimeout bounds the startup check of Redis.
const redisPingTimeout = 2 * time.Second

// shutdownTimeout bounds graceful shutdown. It is longer than a write's 10s
// deadline (internal/httpapi's writeTimeout) plus the 5s a failed amend may
// spend queueing D-057's errand.reward_set (internal/order's queueTimeout), so
// an amend in flight at SIGTERM queues it before the database pool closes.
// compose.yaml's stop_grace_period for order-service must stay above it, or
// Docker kills the process first.
const shutdownTimeout = 20 * time.Second

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("order-service stopped", "err", err)
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

	// One client for both callees; each call sets its own per-attempt timeout.
	httpClient := &http.Client{}
	repo := repository.NewPostgres(pool)
	svc := order.NewService(
		repo,
		repo, // AI-generated (edited by PENDING): the rating store (D-093).
		creditclient.New(cfg.CreditBaseURL, httpClient, log),
		supplierclient.New(cfg.SupplierBaseURL, httpClient, log),
		order.SystemClock{},
		order.Config{
			ExpiryPeriod:  cfg.ExpiryPeriod,
			MaxActive:     cfg.MaxActive,
			ConfirmWindow: cfg.ConfirmWindow,
		},
		log,
	)

	publisher := relay.NewAMQPPublisher(cfg.RabbitMQURL, eventsExchange, log)
	defer func() {
		if err := publisher.Close(); err != nil {
			log.Warn("close broker connection", "err", err)
		}
	}()

	done := make(chan struct{}, 2)
	go func() {
		sweeper.New(repo, svc, order.SystemClock{}, log, sweepInterval).Run(ctx)
		done <- struct{}{}
	}()
	go func() {
		relay.New(relay.NewPostgresStore(pool), publisher, log, relay.Options{}).Run(ctx)
		done <- struct{}{}
	}()

	// Per-service authentication (D-056): keys from user-service, fetched on
	// first use, and revocations from the shared Redis (D-054). Neither has to
	// be up for order-service to start: go-redis connects lazily.
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return errors.New("REDIS_URL could not be parsed")
	}
	rdb := redis.NewClient(redisOpts)
	defer func() {
		if err := rdb.Close(); err != nil {
			log.Warn("close Redis client", "err", err)
		}
	}()
	// AI-generated (edited by James Teo).
	pingRedis(ctx, rdb, log)
	verifier := auth.NewVerifier(cfg.JWKSURL, &http.Client{Timeout: 5 * time.Second})
	// AI-generated (edited by PENDING): Find errands' records (D-092).
	activity := order.NewActivity(repo, order.SystemClock{})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewRouter(svc, activity, pool, verifier, auth.NewRevocations(rdb), log),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Info("order-service listening", "port", cfg.Port)
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
	for range 2 {
		select {
		case <-done:
		case <-shutdownCtx.Done():
			return nil
		}
	}
	log.Info("order-service stopped")
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

// AI-generated (edited by James Teo).
// pingRedis checks Redis once at startup, so a wrong REDIS_URL or password
// shows at deploy time rather than only in each request's log line. A failure
// is logged and startup goes on: Redis may come up later (root AGENTS.md §5).
func pingRedis(ctx context.Context, rdb *redis.Client, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, redisPingTimeout)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("Redis ping at startup failed; while it cannot be reached, revocation checks are skipped, "+
			"and while it answers with an error, every request with a token is refused with 503", "err", err)
	}
}

// migrationsDir prefers ./migrations (go run from order-service/) and falls
// back to the image's /app/migrations, as supplier-service does.
func migrationsDir() string {
	dir, err := filepath.Abs("migrations")
	if err == nil {
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir
		}
	}
	return "/app/migrations"
}
