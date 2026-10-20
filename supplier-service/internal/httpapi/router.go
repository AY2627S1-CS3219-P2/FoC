// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Takes the verifier and revocation checker and guards the write routes
//   with them; the permissive CORS handler is gone, since browsers only reach
//   the gateway.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"foc/supplier-service/internal/supplier"
)

// NewRouter serves the supplier API. Reads need no token. POST, PUT and DELETE
// need an ADMIN's access token, which v verifies and rev checks for revocation
// (D-041, D-054).
func NewRouter(svc *supplier.Service, v TokenVerifier, rev RevocationChecker) http.Handler {
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", h.Health)

	r.Route("/suppliers", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)

		r.Group(func(r chi.Router) {
			r.Use(authenticate(v, rev))
			r.Use(requireAdmin)
			r.Post("/", h.Create)
			r.Put("/{id}", h.Update)
			r.Delete("/{id}", h.Delete)
		})
	})

	return r
}
