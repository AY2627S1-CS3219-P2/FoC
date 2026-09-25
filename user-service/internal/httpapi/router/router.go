// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added dedicated HTTP router setup that delegates route registration.
// Author review: COMPLETED BY ZI YANG

// Package router constructs the user-service HTTP router.
package router

import (
	"net/http"

	"foc/user-service/internal/httpapi/routes"
	"github.com/go-chi/chi/v5"
)

// Setup constructs the configured user-service HTTP router.
func Setup(deps routes.Dependencies) http.Handler {
	router := chi.NewRouter()
	setUpRoutes(router, deps)
	return router
}

func setUpRoutes(router chi.Router, deps routes.Dependencies) {
	router.Group(routes.GetRoutes(deps))
}
