// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated minimal HTTP startup wiring for credit-service health endpoint.
// Author review: Reviewed by Seth

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/config"
	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/httpapi"
	"foc/credit-service/internal/messaging"
	"foc/credit-service/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Set up the logger to print messages in JSON format to the terminal.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Start and set up the app (connect to database, apply tables, start web server).
	// If anything fails during startup or while running, enter the if-block.
	if err := run(context.Background(), config.Load(), logger); err != nil {
		// Print the error message so we know what went wrong.
		logger.Error("credit service stopped", "error", err)

		// Stop the program completely with an error code (1 means failure).
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	// Database Migration
	if err := repository.Apply(cfg.DatabaseURL, "file://migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	// Database Connection Pool
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Repository and service initialization
	creditRepository := repository.NewPostgresRepository(pool)
	creditService := credit.NewService(creditRepository)

	// create JWT token verifier
	verifier := auth.NewVerifier(cfg.JWKSURL, nil)

	// HTTP Server Startup with Chi Router
	server := newHTTPServer(cfg.Addr(), httpapi.NewRouter(pool.Ping, verifier, creditService))

	errCh := make(chan error, 1)
	if cfg.RabbitMQURL != "" {
		registrationHandler := messaging.NewRegistrationHandler(creditService)
		go func() {
			logger.Info("starting registration consumer", "queue", "credit.registration")
			if err := messaging.RunRegistrationConsumer(ctx, cfg.RabbitMQURL, registrationHandler, logger); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- fmt.Errorf("consume registration events: %w", err)
			}
		}()
	}
	go func() {
		logger.Info("starting credit service", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve HTTP: %w", err)
			return
		}
		errCh <- nil
	}()

	// Graceful Shutdown
	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
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
