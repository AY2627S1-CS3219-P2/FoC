// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Mounted generated OpenAPI read and reservation handlers with mandatory Bearer token authentication and public health endpoints per D-041/D-056.
// Author review: PENDING - reviewer to complete

package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/gen"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// HealthCheck reports whether a required runtime dependency is reachable.
type HealthCheck func(context.Context) error

// NewRouter returns the credit-service HTTP handler, logging every request to
// logger. A nil logger discards the lines.
func NewRouter(healthCheck HealthCheck, verifier *auth.Verifier, rev *auth.Revocations, creditService *credit.Service, logger *slog.Logger) http.Handler {
	logger = orDiscard(logger)
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	// The access log wraps the recoverer so it records the 500 a panic becomes.
	router.Use(accessLog(logger))
	router.Use(recoverPanics(logger))

	router.Get("/api/v1/health", health(healthCheck, logger))

	if creditService != nil {
		h := NewHandler(creditService, healthCheck, logger)
		gen.HandlerWithOptions(h, gen.ChiServerOptions{
			BaseRouter: router,
			Middlewares: []gen.MiddlewareFunc{
				AuthenticateRoutes(verifier, rev, logger),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				writeError(w, http.StatusBadRequest, gen.INVALIDREQUEST, err.Error())
			},
		})
	}

	return router
}

// AuthenticateRoutes returns a middleware that enforces Bearer token verification
// and revocation checks on all protected API routes, skipping public health endpoints.
func AuthenticateRoutes(verifier *auth.Verifier, rev *auth.Revocations, logger *slog.Logger) gen.MiddlewareFunc {
	logger = orDiscard(logger)
	if verifier == nil {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				logCause(logger, r, "no token verifier is configured; refusing request", nil)
				writeError(w, http.StatusServiceUnavailable, gen.SERVICEUNAVAILABLE, "verifier unconfigured")
			})
		}
	}

	tokenAuth := RequireToken(verifier, rev, logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/health/") || r.URL.Path == "/api/v1/health" {
				next.ServeHTTP(w, r)
				return
			}
			tokenAuth(next).ServeHTTP(w, r)
		})
	}
}

func health(healthCheck HealthCheck, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if healthCheck != nil {
			if err := healthCheck(r.Context()); err != nil {
				logCause(logger, r, "health check failed", err)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
