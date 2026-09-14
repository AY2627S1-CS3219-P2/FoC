package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	appmw "foc/supplier-service/internal/middleware"
	"foc/supplier-service/internal/supplier"
)

func NewRouter(svc *supplier.Service) http.Handler {
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Permissive CORS: the frontend is a separate static origin and there's
	// no browser-facing auth secret yet (see internal/middleware/auth.go),
	// so this is fine for local/dev use.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "X-User-Role"},
	}))

	r.Get("/health", h.Health)

	r.Route("/suppliers", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)

		r.Group(func(r chi.Router) {
			r.Use(appmw.RequireAdmin(appmw.HeaderRoleExtractor))
			r.Post("/", h.Create)
			r.Put("/{id}", h.Update)
			r.Delete("/{id}", h.Delete)
		})
	})

	return r
}
