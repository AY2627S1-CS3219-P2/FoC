// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added dedicated HTTP router setup that delegates route registration.
// Author review: COMPLETED BY ZI YANG

// Package router constructs the user-service HTTP router.
package router

import (
	"net/http"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Setup constructs the configured user-service HTTP router.
func Setup(deps routes.Dependencies) http.Handler {
	router := chi.NewRouter()
	// The access log wraps the recoverer, so a recovered panic is logged as the
	// 500 it became; both sit inside RequestID to carry the request's id.
	router.Use(middleware.RequestID, handlers.AccessLog(deps.Logger), handlers.RecoverPanics(deps.Logger))
	setUpRoutes(router, deps)
	return router
}

func setUpRoutes(router chi.Router, deps routes.Dependencies) {
	router.Group(routes.GetRoutes(deps))
}
