// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated minimal Chi router with health endpoint for credit-service startup.
// Author review: PENDING - reviewer to complete

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// HealthCheck reports whether a required runtime dependency is reachable.
type HealthCheck func(context.Context) error

// NewRouter returns the credit-service HTTP handler.
func NewRouter(healthCheck HealthCheck) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	router.Get("/api/v1/health", health(healthCheck))

	return router
}

func health(healthCheck HealthCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if healthCheck != nil {
			if err := healthCheck(r.Context()); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
