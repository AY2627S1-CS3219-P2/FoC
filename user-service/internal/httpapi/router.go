// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the Chi router and HTTP handler scaffold for recorded user-service routes.
// Author review: COMPLETED BY ZI YANG

// Package httpapi contains the user-service HTTP transport layer.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter constructs the user-service HTTP router.
func NewRouter(deps Dependencies) http.Handler {
	h := handler{deps: deps}
	r := chi.NewRouter()
	r.Get("/.well-known/jwks.json", h.jwks)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", h.health)
		r.Post("/users/register", h.register)
		r.Post("/users/login", h.login)
		r.Post("/users/refresh", h.refresh)
		r.Post("/users/logout", h.logout)
		r.With(RequireJWT(h.deps.TokenVerifier)).Get("/users/{uid}", h.profile)
		r.With(RequireJWT(h.deps.TokenVerifier)).Put("/users/{uid}", h.updateProfile)
		r.With(RequireJWT(h.deps.TokenVerifier)).Patch("/users/{uid}/status", h.updateStatus)
	})
	return r
}
