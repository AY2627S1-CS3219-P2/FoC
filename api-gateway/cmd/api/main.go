// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Process wiring for the gateway scaffold — config load, HTTP server,
//   graceful shutdown, health endpoint. No routing, auth or proxying.
// Author review: Edited by nigeltzy

// Command api is the FoC API Gateway (D-010): the only publicly reachable
// process in the system.
//
// SCAFFOLD ONLY. It starts, serves /healthz, and stops cleanly. It does not
// verify tokens, check revocation or forward anything — see the doc.go in each
// internal package for what blocks it.
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

	"foc/api-gateway/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("api-gateway: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
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
