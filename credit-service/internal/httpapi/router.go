// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Mounted generated OpenAPI read and reservation handlers with Bearer token authentication and health endpoints per sourceOfTruth.md Phase 9.
// Author review: PENDING - reviewer to complete

package httpapi

import (
	"context"
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

// NewRouter returns the credit-service HTTP handler.
func NewRouter(healthCheck HealthCheck, verifier *auth.Verifier, creditService *credit.Service) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	router.Get("/api/v1/health", health(healthCheck))

	if creditService != nil {
		h := NewHandler(creditService, healthCheck)
		gen.HandlerWithOptions(h, gen.ChiServerOptions{
			BaseRouter: router,
			Middlewares: []gen.MiddlewareFunc{
				AuthenticateReadRoutes(verifier),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				writeError(w, http.StatusBadRequest, gen.ErrorCodeINVALIDREQUEST, err.Error())
			},
		})
	}

	return router
}

// AuthenticateReadRoutes returns a middleware that applies Bearer token verification
// exclusively to authenticated end-user read routes (/api/v1/credits/accounts/me*).
func AuthenticateReadRoutes(verifier *auth.Verifier) gen.MiddlewareFunc {
	var tokenAuth func(http.Handler) http.Handler
	if verifier != nil {
		tokenAuth = RequireToken(verifier)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/credits/accounts/me") {
				if tokenAuth != nil {
					tokenAuth(next).ServeHTTP(w, r)
					return
				}
				writeError(w, http.StatusUnauthorized, gen.ErrorCodeINVALIDREQUEST, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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
