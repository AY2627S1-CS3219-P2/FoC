package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	appmw "foc/supplier-service/internal/middleware"
	"foc/supplier-service/internal/supplier"
)

func NewRouter(svc *supplier.Service) http.Handler {
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

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
