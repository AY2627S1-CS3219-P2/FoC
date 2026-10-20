// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added route-registration coverage for the dedicated routes package.
// Author review: COMPLETED BY ZI YANG - validate correctness of test implementation

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type revokedTokenVerifier struct{}

func (revokedTokenVerifier) Verify(context.Context, string) (handlers.Principal, error) {
	return handlers.Principal{UserID: uuid.New(), JTI: uuid.New()}, nil
}

type revokedTokenChecker struct{}

func (revokedTokenChecker) Check(context.Context, handlers.Principal) (bool, error) {
	return true, nil
}

// AI-generated (edited by ZI YANG): exact statuses expose an incorrectly registered HTTP method as a test failure.
func TestGetRoutesRegistersRecordedEndpoints(t *testing.T) {
	router := chi.NewRouter()
	router.Group(GetRoutes(Dependencies{}))

	tests := []struct {
		method     string
		path       string
		wantStatus int
	}{
		{http.MethodGet, "/.well-known/jwks.json", http.StatusInternalServerError},
		{http.MethodGet, "/api/v1/health", http.StatusOK},
		{http.MethodPost, "/api/v1/users/register", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/users/login", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/users/refresh", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/users/logout", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/users/" + uuid.NewString(), http.StatusInternalServerError},
		{http.MethodPut, "/api/v1/users/" + uuid.NewString(), http.StatusInternalServerError},
		{http.MethodPatch, "/api/v1/users/" + uuid.NewString() + "/status", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d for %s %s", response.Code, tt.wantStatus, tt.method, tt.path)
			}
		})
	}
}

func TestGetRoutesRejectsRevokedTokensOnEveryProtectedRoute(t *testing.T) {
	router := chi.NewRouter()
	router.Group(GetRoutes(Dependencies{
		TokenVerifier:     revokedTokenVerifier{},
		RevocationChecker: revokedTokenChecker{},
	}))

	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/users/" + uuid.NewString()},
		{method: http.MethodPut, path: "/api/v1/users/" + uuid.NewString()},
		{method: http.MethodPatch, path: "/api/v1/users/" + uuid.NewString() + "/status"},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer verified")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
		})
	}
}

// countingRevokedChecker reports every token as revoked and counts the checks.
type countingRevokedChecker struct{ calls int }

func (c *countingRevokedChecker) Check(context.Context, handlers.Principal) (bool, error) {
	c.calls++
	return true, nil
}

// Logout is not behind the revocation check: a client logging out with a token
// that is already revoked must still reach the logout handler, which answers
// it idempotently.
func TestLogoutIsNotBehindTheRevocationCheck(t *testing.T) {
	checker := &countingRevokedChecker{}
	router := chi.NewRouter()
	router.Group(GetRoutes(Dependencies{
		TokenVerifier:     revokedTokenVerifier{},
		RevocationChecker: checker,
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/logout", nil)
	request.Header.Set("Authorization", "Bearer verified")

	router.ServeHTTP(httptest.NewRecorder(), request)

	if checker.calls != 0 {
		t.Fatalf("revocation checks on logout = %d, want 0", checker.calls)
	}
}
