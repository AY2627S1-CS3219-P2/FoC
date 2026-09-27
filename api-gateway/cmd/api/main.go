// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Process wiring — config load, verifier, router, HTTP server,
//   graceful shutdown.
// Author review: Edited by nigeltzy - I checked the output of this generated code and asked for some explanation as this was generated alongside the proxy package that I had guided the AI to create. The generated code seems to be a typical implementation of a main.go file for an API gateway, with proper error handling and graceful shutdown.
//
// 2026-09-19: rewritten from the scaffold's health-only server to wire the
// real router (D-027). Re-read before relying on the sign-off above.

// Command api runs the FoC API Gateway. It verifies each access token against
// user-service's JWKS, strips client-supplied claim headers, injects its own,
// forwards the request to the downstream service over HTTP, and serves the
// built frontend when STATIC_DIR is set.
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
