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
	"github.com/google/uuid"
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
				AuthenticateRoutes(verifier),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				writeError(w, http.StatusBadRequest, gen.INVALIDREQUEST, err.Error())
			},
		})

		// Mount internal alias routes for backward compatibility
		router.Post("/internal/v1/credit-reservations", h.CreateReservation)
		router.Patch("/internal/v1/credit-reservations/{errandId}", func(w http.ResponseWriter, r *http.Request) {
			errandIDStr := chi.URLParam(r, "errandId")
			errID, err := uuid.Parse(errandIDStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, gen.INVALIDREQUEST, "invalid errand ID")
				return
			}
			h.AdjustReservationReward(w, r, errID)
		})
	}

	return router
}

// AuthenticateRoutes returns a middleware that enforces Bearer token verification
// on /api/v1/credits/accounts/me*, and optionally extracts caller identity if a Bearer
// token is provided on other routes (e.g. reservation creation from order-service).
func AuthenticateRoutes(verifier *auth.Verifier) gen.MiddlewareFunc {
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
				writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "unauthorized")
				return
			}

			// For reservation or other routes, if Authorization Bearer token is present, decode it into context
			if r.Header.Get("Authorization") != "" && tokenAuth != nil {
				tokenAuth(next).ServeHTTP(w, r)
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
