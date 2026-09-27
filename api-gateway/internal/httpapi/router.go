// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: The gateway's route table and router construction. Implements the
//   public surface recorded as D-027. 2026-09-21: /api/users now targets
//   user-service's real /api/v1/users prefix and keeps the bearer token.
//   2026-09-22: /api/suppliers likewise targets supplier-service's real
//   /suppliers prefix, now that PR #1 has put that router on main, and the
//   interim permissive CORS policy was added so the browser can reach the
//   gateway cross-origin at all. Later that day the browser stopped being
//   cross-origin — the frontend proxies to here — so that policy was deleted
//   again, and the auth routes gained one constructor each for the refresh
//   token's cookie.
// Author review: Nigeltzy - Directed and checked the ouput of the AI code, intention and
//   changes are as seen above. I also checked externally to see if practices
//   such as importing third-party packages were reasonable and safe.

// Package httpapi holds the gateway's router and its transport-only handlers.
//
// Adding a downstream service takes a base URL in internal/config and one
// entry in NewRouter's serviceRoutes table.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/config"
	"foc/api-gateway/internal/proxy"
)

// userServiceAPIPrefix is where user-service mounts its routes. Both the
// public /auth/* routes and /api/users/* are rewritten onto it.
const userServiceAPIPrefix = "/api/v1/users"

// supplierServiceAPIPrefix is where supplier-service mounts its routes; the
// public /api/suppliers/* path is rewritten onto it.
const supplierServiceAPIPrefix = "/suppliers"

// authRoute is one of the four public /auth routes. They carry credentials,
// not a verified identity, so they are registered outside RequireToken and
// user-service authenticates them. newProxy sets the route's refresh-cookie
// handling: none for register, set on login, read and replaced on refresh,
// read and cleared on logout.
type authRoute struct {
	name     string
	newProxy func(proxy.Route, time.Duration) (*proxy.Proxy, error)
}

func authRoutes() []authRoute {
	return []authRoute{
		{"register", func(r proxy.Route, _ time.Duration) (*proxy.Proxy, error) {
			return proxy.NewPassthrough(r)
		}},
		{"login", proxy.NewSessionIssuing},
		{"refresh", proxy.NewSessionRotating},
		{"logout", func(r proxy.Route, _ time.Duration) (*proxy.Proxy, error) {
			return proxy.NewSessionEnding(r)
		}},
	}
}

// NewRouter builds the gateway's handler: /healthz, the four public /auth
// routes, the /api/<service> prefixes behind RequireToken and, when staticDir
// is non-empty, the built frontend for every path no route matches. Clients
// call these public paths, so renaming one, or moving one out of RequireToken,
// changes the gateway's API (D-027 in ai/decisions.md).
func NewRouter(downstream config.Downstream, verifier *auth.Verifier, refreshTokenTTL time.Duration, staticDir string) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// AI-generated (edited by nigeltzy).
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// The gateway's own liveness check; not proxied.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Public auth routes: outside RequireToken, rewritten onto user-service.
	for _, route := range authRoutes() {
		public := "/auth/" + route.name
		p, err := route.newProxy(proxy.Route{
			BaseURL:     downstream.User,
			StripPrefix: "/auth",
			AddPrefix:   userServiceAPIPrefix,
		}, refreshTokenTTL)
		if err != nil {
			return nil, fmt.Errorf("httpapi: auth route %s: %w", public, err)
		}
		r.Method(http.MethodPost, public, p)
	}

	// Authenticated service routes, each behind RequireToken. A row strips its
	// public prefix and prepends addPrefix (empty forwards the rest unchanged);
	// newProxy decides whether Authorization reaches the callee.
	serviceRoutes := []struct {
		prefix    string
		baseURL   string
		addPrefix string
		newProxy  func(proxy.Route) (*proxy.Proxy, error)
	}{
		// user-service verifies the bearer token itself, so its row keeps
		// Authorization.
		{"/api/users", downstream.User, userServiceAPIPrefix, proxy.NewRetainingToken},
		{"/api/suppliers", downstream.Supplier, supplierServiceAPIPrefix, proxy.New},
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

	// AI-generated (edited by nigeltzy).
	// Unmatched paths under /api and /auth get a JSON 404 rather than the
	// frontend, so a mistyped API call fails as one instead of returning a page.
	apiNotFound := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such route")
	}
	for _, prefix := range []string{"/api", "/auth"} {
		r.HandleFunc(prefix, apiNotFound)
		r.HandleFunc(prefix+"/*", apiNotFound)
	}

	// Other paths no route matches get the built frontend; with an empty
	// staticDir they get chi's default 404.
	if staticDir != "" {
		r.NotFound(newSPAHandler(staticDir).ServeHTTP)
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
