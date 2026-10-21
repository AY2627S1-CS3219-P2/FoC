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
//   token's cookie. 2026-10-10 (Claude Code, Opus 5.5): /auth/resend-otp and
//   /auth/verify-otp added as passthroughs for user-service's OTP endpoints
//   (PR #14), recorded as D-043.
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
	"errors"
	"fmt"
	"net/http"
	// AI-generated (edited by James Teo).
	"strings"
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

// AI-generated (edited by James Teo).
// creditServiceAccountsPrefix is where credit-service mounts its account
// routes; the public /api/credits/accounts/* path is rewritten onto it.
const creditServiceAccountsPrefix = "/api/v1/credits/accounts"

// authRoute is one of the six public /auth routes. They carry credentials,
// not a verified identity, so they are registered outside RequireToken and
// user-service authenticates them. newProxy sets the route's refresh-cookie
// handling: none for register and the two OTP routes, set on login, read and
// replaced on refresh, read and cleared on logout.
type authRoute struct {
	name     string
	newProxy func(proxy.Route, time.Duration) (*proxy.Proxy, error)
}

// passthrough is the constructor for auth routes that issue no tokens and
// read no refresh cookie.
func passthrough(r proxy.Route, _ time.Duration) (*proxy.Proxy, error) {
	return proxy.NewPassthrough(r)
}

func authRoutes() []authRoute {
	return []authRoute{
		{"register", passthrough},
		// AI-generated (edited by nigeltzy): the OTP routes added by D-043.
		// verify-otp only activates the account and returns no tokens, so the
		// client logs in afterwards through /auth/login.
		{"resend-otp", passthrough},
		{"verify-otp", passthrough},
		{"login", proxy.NewSessionIssuing},
		{"refresh", proxy.NewSessionRotating},
		{"logout", func(r proxy.Route, _ time.Duration) (*proxy.Proxy, error) {
			return proxy.NewSessionEnding(r)
		}},
	}
}

// AI-generated (edited by James Teo).
// NewRouter builds the gateway's handler: /healthz, the six public /auth
// routes and the /api/<service> prefixes behind RequireToken. Clients call
// these public paths, so renaming one, or moving one out of RequireToken,
// changes the gateway's API (D-027, D-043 and D-056 in ai/decisions.md).
// revocations is consulted on every token RequireToken verifies (D-054).
func NewRouter(downstream config.Downstream, verifier *auth.Verifier, revocations RevocationChecker, refreshTokenTTL time.Duration) (http.Handler, error) {
	if revocations == nil {
		return nil, errors.New("httpapi: NewRouter needs a revocation checker")
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// AI-generated (edited by nigeltzy).
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// AI-generated (edited by James Teo).
	// Ahead of every route, and after the logger so a refused path is logged.
	r.Use(rejectDotSegments)

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
		// AI-generated (edited by James Teo).
		// D-041 has every service verify the bearer token itself, so these rows
		// keep Authorization too.
		// The claim headers are still stripped and injected (D-022).
		{"/api/suppliers", downstream.Supplier, supplierServiceAPIPrefix, proxy.NewRetainingToken},
		{"/api/orders", downstream.Order, "", proxy.NewRetainingToken},
		// Only credit-service's account routes are public (D-056). Its
		// reservation routes are for order-service alone, so nothing else
		// under /api/credits is routed and it gets the JSON 404 below.
		{"/api/credits/accounts", downstream.Credit, creditServiceAccountsPrefix, proxy.NewRetainingToken},
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
			gr.Use(RequireToken(verifier, revocations))
			// Both forms: the bare prefix and everything beneath it.
			gr.Handle(route.prefix, p)
			gr.Handle(route.prefix+"/*", p)
		})
	}

	// AI-generated (edited by nigeltzy).
	// Unmatched paths under /api and /auth get a JSON 404 in the gateway's
	// error shape, so a mistyped API call fails the way the client expects.
	apiNotFound := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such route")
	}
	for _, prefix := range []string{"/api", "/auth"} {
		r.HandleFunc(prefix, apiNotFound)
		r.HandleFunc(prefix+"/*", apiNotFound)
	}

	return r, nil
}

// AI-generated (edited by James Teo).
// rejectDotSegments answers the gateway's JSON 404, before any route
// matches, when a segment of the decoded path is "." or "..". Routes forward
// the rest of the path without cleaning it, so without this check
// /api/credits/accounts/../reservations, raw or percent-encoded, reaches
// credit-service with its dot segments intact (percent-encoded ones decoded
// to a literal ..), and a service that resolves dot segments would answer it
// from the reservation routes D-056 keeps from the browser.
func rejectDotSegments(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, segment := range strings.Split(r.URL.Path, "/") {
			if segment == "." || segment == ".." {
				writeError(w, http.StatusNotFound, "no such route")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// writeError renders the gateway's own error shape. Downstream errors pass
// through untouched — this is only for failures the gateway itself decides.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
