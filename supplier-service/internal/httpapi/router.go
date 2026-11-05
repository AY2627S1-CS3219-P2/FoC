// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Takes the verifier and revocation checker and guards the write routes
//   with them; the permissive CORS handler is gone, since browsers only reach
//   the gateway.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"foc/supplier-service/internal/supplier"
)

// NewRouter serves the supplier API and logs every request to log. Reads need
// no token. POST, PUT and DELETE need an ADMIN's access token, which v verifies
// and rev checks for revocation (D-041, D-054).
func NewRouter(svc *supplier.Service, v TokenVerifier, rev RevocationChecker, log *slog.Logger) http.Handler {
	h := NewHandler(svc, log)
	r := chi.NewRouter()
	// The request id is outermost so every line about the request carries it;
	// the access log wraps recovery so it records the 500 a panic becomes.
	r.Use(middleware.RequestID)
	r.Use(accessLog(log))
	r.Use(recoverPanics(log))

	r.Get("/health", h.Health)

	r.Route("/suppliers", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)

		r.Group(func(r chi.Router) {
			r.Use(authenticate(v, rev, log))
			r.Use(requireAdmin(log))
			r.Get("/audit-log", h.AuditLog)
			r.Post("/", h.Create)
			r.Put("/{id}", h.Update)
			r.Delete("/{id}", h.Delete)
		})
	})

	return r
}
