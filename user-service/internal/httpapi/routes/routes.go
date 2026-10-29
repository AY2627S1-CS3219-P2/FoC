// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added route registration that constructs the grouped HTTP handlers from their dependencies.
// Author review: COMPLETED BY ZI YANG

// Package routes assembles endpoint handlers into the user-service HTTP route tree.
package routes

import (
	"foc/user-service/internal/httpapi/handlers"

	"github.com/go-chi/chi/v5"
)

// Dependencies contains the grouped handler dependencies required to register routes.
type Dependencies struct {
	Auth              handlers.AuthDependencies
	Profile           handlers.ProfileDependencies
	System            handlers.SystemDependencies
	TokenVerifier     handlers.TokenVerifier
	RevocationChecker handlers.RevocationChecker

	// AI-generated (edited by PENDING): the ranking-model store (D-095, D-099).
	Ranking handlers.RankingDependencies
}

// GetRoutes constructs grouped handlers and returns their route-registration function.
func GetRoutes(deps Dependencies) func(chi.Router) {
	authHandler := handlers.NewAuthHandler(deps.Auth)
	profileHandler := handlers.NewProfileHandler(deps.Profile)
	systemHandler := handlers.NewSystemHandler(deps.System)
	rankingHandler := handlers.NewRankingHandler(deps.Ranking)

	return func(router chi.Router) {
		router.Get("/.well-known/jwks.json", systemHandler.JWKS)
		router.Route("/api/v1", func(router chi.Router) {
			router.Get("/health", systemHandler.Health)
			router.Post("/users/register", authHandler.Register)
			router.Post("/users/resend-otp", authHandler.ResendOTP)
			router.Post("/users/verify-otp", authHandler.VerifyOTP)
			router.Post("/users/login", authHandler.Login)
			router.Post("/users/refresh", authHandler.Refresh)
			router.Post("/users/logout", authHandler.Logout)
			router.With(handlers.RequireJWT(deps.TokenVerifier, deps.RevocationChecker, deps.Auth.Logger)).Get("/users/{uid}", profileHandler.Profile)
			router.With(handlers.RequireJWT(deps.TokenVerifier, deps.RevocationChecker, deps.Auth.Logger)).Put("/users/{uid}", profileHandler.UpdateProfile)
			router.With(handlers.RequireJWT(deps.TokenVerifier, deps.RevocationChecker, deps.Auth.Logger)).Patch("/users/{uid}/status", profileHandler.UpdateStatus)
			// AI-generated (edited by PENDING): each user's own ranking model (D-095, D-099).
			router.With(handlers.RequireJWT(deps.TokenVerifier, deps.RevocationChecker, deps.Auth.Logger)).Get("/users/{uid}/ranking-model", rankingHandler.Get)
			router.With(handlers.RequireJWT(deps.TokenVerifier, deps.RevocationChecker, deps.Auth.Logger)).Put("/users/{uid}/ranking-model", rankingHandler.Put)
		})
	}
}
