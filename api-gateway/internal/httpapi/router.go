// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: The gateway's route table and router construction. Implements the
//   public surface recorded as D-027. 2026-09-21: /api/users now targets
//   user-service's real /api/v1/users prefix and keeps the bearer token.
// Author review: PENDING — <reviewer to complete>

// Package httpapi holds the gateway's router and its transport-only handlers.
//
// Adding a service to the gateway is meant to be a small, local change: give
// it a base URL in internal/config, then add one entry to serviceRoutes below.
// Nothing else in the gateway needs to know it exists (D-027).
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/config"
	"foc/api-gateway/internal/proxy"
)

// userServiceAPIPrefix is where user-service actually mounts its routes. The
// gateway's public /auth/* paths are rewritten onto it, so the browser never
// sees the internal layout (D-027).
const userServiceAPIPrefix = "/api/v1/users"

// authRoutes are the four public routes the gateway terminates. They take
// credentials rather than a verified identity, so they are NOT behind
// RequireToken — user-service authenticates them itself.
var authRoutes = []string{"register", "login", "refresh", "logout"}

// NewRouter builds the gateway's handler.
//
// It returns a ready-to-serve handler or an error; there is nothing to start
// afterwards (root AGENTS.md §5).
func NewRouter(downstream config.Downstream, verifier *auth.Verifier) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// The gateway's own liveness check. Not proxied anywhere.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// ---- Public auth routes, rewritten onto user-service ----------------
	for _, name := range authRoutes {
		public := "/auth/" + name
		p, err := proxy.NewPassthrough(proxy.Route{
			BaseURL:     downstream.User,
			StripPrefix: "/auth",
			AddPrefix:   userServiceAPIPrefix,
		})
		if err != nil {
			return nil, fmt.Errorf("httpapi: auth route %s: %w", public, err)
		}
		r.Method(http.MethodPost, public, p)
	}

	// ---- Authenticated service routes -----------------------------------
	// THE EXTENSION POINT. One entry per service; the middleware, the header
	// strip and the injection come for free.
	//
	// addPrefix is where the callee actually mounts its routes, and it is not
	// guesswork: it is read off that service's committed api/openapi.yaml. An
	// empty one means the service serves its resources at the root, which is
	// what the public path maps to once its own prefix is stripped.
	//
	// newProxy is the callee's credential handling. A named constructor per
	// row rather than a bool in the struct, so the table says what it wants
	// instead of selecting a branch inside the proxy (root AGENTS.md §5).
	serviceRoutes := []struct {
		prefix    string
		baseURL   string
		addPrefix string
		newProxy  func(proxy.Route) (*proxy.Proxy, error)
	}{
		// user-service serves /api/v1/users/{uid} and authenticates on the
		// bearer token itself, so this row differs from the others twice over.
		{"/api/users", downstream.User, userServiceAPIPrefix, proxy.NewRetainingToken},
		// NOTE: supplier-service mounts at /suppliers/{id}, not at the root,
		// so this row is wrong in the same way /api/users was — GET
		// /api/suppliers/42 reaches it as /42 and 404s. Left alone on purpose:
		// that is its owner's contract to confirm, not one to infer from here
		// (root AGENTS.md §3). Flagged for Goh Chee Yang.
		{"/api/suppliers", downstream.Supplier, "", proxy.New},
		{"/api/orders", downstream.Order, "", proxy.New},
		{"/api/credits", downstream.Credit, "", proxy.New},
	}

	for _, route := range serviceRoutes {
		p, err := route.newProxy(proxy.Route{
			BaseURL:     route.baseURL,
			StripPrefix: route.prefix,
			AddPrefix:   route.addPrefix,
		})
		if err != nil {
			return nil, fmt.Errorf("httpapi: route %s: %w", route.prefix, err)
		}

		r.Group(func(gr chi.Router) {
			gr.Use(RequireToken(verifier))
			// Both forms: the bare prefix and everything beneath it.
			gr.Handle(route.prefix, p)
			gr.Handle(route.prefix+"/*", p)
		})
	}

	return r, nil
}

// writeError renders the gateway's own error shape. Downstream errors pass
// through untouched — this is only for failures the gateway itself decides.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
