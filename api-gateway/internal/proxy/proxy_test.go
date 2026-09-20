// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Unit tests for path rewriting and the D-022 header strip-and-inject.
// Author review: PENDING — <reviewer to complete>

package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"foc/api-gateway/internal/proxy"
)

func TestRewritePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route proxy.Route
		in    string
		want  string
	}{
		{
			name:  "auth route is rewritten onto the service's own prefix",
			route: proxy.Route{StripPrefix: "/auth", AddPrefix: "/api/v1/users"},
			in:    "/auth/login",
			want:  "/api/v1/users/login",
		},
		{
			name:  "service route keeps the remainder",
			route: proxy.Route{StripPrefix: "/api/suppliers"},
			in:    "/api/suppliers/42",
			want:  "/42",
		},
		{
			name:  "bare prefix becomes root",
			route: proxy.Route{StripPrefix: "/api/suppliers"},
			in:    "/api/suppliers",
			want:  "/",
		},
		{
			name:  "bare prefix with AddPrefix does not gain a trailing slash",
			route: proxy.Route{StripPrefix: "/auth", AddPrefix: "/api/v1/users"},
			in:    "/auth",
			want:  "/api/v1/users",
		},
		{
			name:  "nested path survives intact",
			route: proxy.Route{StripPrefix: "/api/orders"},
			in:    "/api/orders/7/items/3",
			want:  "/7/items/3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.route.RewritePath(tt.in)
			if got != tt.want {
				t.Errorf("rewritePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestStripsClientSuppliedClaimHeaders is the test that matters most in this
// package. D-022 lets downstream services trust the claim headers precisely
// because a caller cannot set them. If this test ever fails, every service's
// access control is bypassable by anyone who can reach the gateway.
func TestStripsClientSuppliedClaimHeaders(t *testing.T) {
	t.Parallel()

	var got http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	p, err := proxy.New(proxy.Route{BaseURL: downstream.URL, StripPrefix: "/api/suppliers"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil)
	// The attack: a caller asserting its own identity.
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")
	req.Header.Set(proxy.ClaimHeaderUserID, "somebody-elses-id")
	req.Header.Set("Authorization", "Bearer a.b.c")

	// What the gateway actually verified.
	ctx := proxy.WithIdentity(req.Context(), proxy.Identity{UserID: "real-uid", Role: "STUDENT"})
	p.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))

	if role := got.Get(proxy.ClaimHeaderRole); role != "STUDENT" {
		t.Errorf("role header = %q, want %q — client-supplied ADMIN was not stripped", role, "STUDENT")
	}
	if uid := got.Get(proxy.ClaimHeaderUserID); uid != "real-uid" {
		t.Errorf("user id header = %q, want %q", uid, "real-uid")
	}
	if authz := got.Get("Authorization"); authz != "" {
		t.Errorf("Authorization = %q, want it stripped on an authenticated route", authz)
	}
}

// An unauthenticated request must reach a downstream service with NO claim
// headers at all, rather than empty ones a service might read as a value.
func TestUnauthenticatedRequestGetsNoClaimHeaders(t *testing.T) {
	t.Parallel()

	var got http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	p, err := proxy.New(proxy.Route{BaseURL: downstream.URL, StripPrefix: "/api/suppliers"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/1", nil)
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")
	// No identity on the context: nothing was verified.
	p.ServeHTTP(httptest.NewRecorder(), req)

	if _, present := got[http.CanonicalHeaderKey(proxy.ClaimHeaderRole)]; present {
		t.Errorf("role header present on an unauthenticated request: %q", got.Get(proxy.ClaimHeaderRole))
	}
}

// The public auth routes must keep Authorization: logout needs the access
// token to reach user-service so it can blocklist that jti.
func TestPassthroughPreservesAuthorization(t *testing.T) {
	t.Parallel()

	var got http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	p, err := proxy.NewPassthrough(proxy.Route{
		BaseURL:     downstream.URL,
		StripPrefix: "/auth",
		AddPrefix:   "/api/v1/users",
	})
	if err != nil {
		t.Fatalf("NewPassthrough: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer a.b.c")
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")
	p.ServeHTTP(httptest.NewRecorder(), req)

	if authz := got.Get("Authorization"); authz != "Bearer a.b.c" {
		t.Errorf("Authorization = %q, want it preserved on the auth routes", authz)
	}
	// Even here the claim headers are stripped: nothing is verified yet.
	if _, present := got[http.CanonicalHeaderKey(proxy.ClaimHeaderRole)]; present {
		t.Error("claim header survived onto a passthrough route")
	}
}

func TestNewRejectsUnusableBaseURL(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "not-a-url", "/relative/only"} {
		if _, err := proxy.New(proxy.Route{BaseURL: bad}); err == nil {
			t.Errorf("New(%q) returned no error, want one", bad)
		}
	}
}

// AI-generated (edited by <name>).
func TestRetainingTokenKeepsAuthorizationAndStillInjects(t *testing.T) {
	t.Parallel()

	var got http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	p, err := proxy.NewRetainingToken(proxy.Route{
		BaseURL:     downstream.URL,
		StripPrefix: "/api/users",
		AddPrefix:   "/api/v1/users",
	})
	if err != nil {
		t.Fatalf("NewRetainingToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/users/uid-1", nil)
	req.Header.Set("Authorization", "Bearer a.b.c")
	// What a malicious client sends. It must not survive.
	req.Header.Set(proxy.ClaimHeaderRole, "ADMIN")
	req = req.WithContext(proxy.WithIdentity(
		req.Context(),
		proxy.Identity{UserID: "uid-1", Role: "STUDENT"},
	))
	p.ServeHTTP(httptest.NewRecorder(), req)

	if authz := got.Get("Authorization"); authz != "Bearer a.b.c" {
		t.Errorf("Authorization = %q, want it retained for user-service", authz)
	}
	if role := got.Get(proxy.ClaimHeaderRole); role != "STUDENT" {
		t.Errorf("%s = %q, want STUDENT — the strip must beat the client's value",
			proxy.ClaimHeaderRole, role)
	}
	if uid := got.Get(proxy.ClaimHeaderUserID); uid != "uid-1" {
		t.Errorf("%s = %q, want uid-1", proxy.ClaimHeaderUserID, uid)
	}
}
