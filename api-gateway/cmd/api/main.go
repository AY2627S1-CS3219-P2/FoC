// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Process wiring — config load, verifier, router, HTTP server,
//   graceful shutdown.
// Author review: Edited by nigeltzy
//
// 2026-09-19: rewritten from the scaffold's health-only server to wire the
// real router (D-027). Re-read before relying on the sign-off above.

// Command api is the FoC API Gateway (D-010): the only publicly reachable
// process in the system.
//
// It verifies an access token's RS256 signature against user-service's JWKS
// (D-023), strips any claim headers the caller sent, injects its own, and
// forwards over synchronous REST (D-013, D-022). It does not talk to Redis
// and does not check revocation (D-024).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/config"
	"foc/api-gateway/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("api-gateway: %v", err)
	}

	// Keys are fetched lazily on first use, not here: the gateway must not
	// assume user-service is already up (root AGENTS.md §5).
	verifier := auth.NewVerifier(cfg.JWKSURL, &http.Client{Timeout: 5 * time.Second})

	router, err := httpapi.NewRouter(cfg.Downstream, verifier, cfg.RefreshTokenTTL, cfg.StaticDir)
	if err != nil {
		log.Fatalf("api-gateway: building router: %v", err)
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Serve in the background so the main goroutine can wait for a signal.
	errs := make(chan error, 1)
	go func() {
		log.Printf("api-gateway: listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errs:
		log.Fatalf("api-gateway: serve: %v", err)
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("api-gateway: shutdown: %v", err)
	}
}
