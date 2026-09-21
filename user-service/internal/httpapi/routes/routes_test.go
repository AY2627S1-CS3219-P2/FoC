// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added route-registration coverage for the dedicated routes package.
// Author review: COMPLETED BY ZI YANG

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestGetRoutesRegistersRecordedEndpoints(t *testing.T) {
	router := chi.NewRouter()
	router.Group(GetRoutes(Dependencies{}))

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/.well-known/jwks.json"},
		{http.MethodGet, "/api/v1/health"},
		{http.MethodPost, "/api/v1/users/register"},
		{http.MethodPost, "/api/v1/users/login"},
		{http.MethodPost, "/api/v1/users/refresh"},
		{http.MethodPost, "/api/v1/users/logout"},
		{http.MethodGet, "/api/v1/users/" + uuid.NewString()},
		{http.MethodPut, "/api/v1/users/" + uuid.NewString()},
		{http.MethodPatch, "/api/v1/users/" + uuid.NewString() + "/status"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code == http.StatusNotFound {
				t.Fatalf("route returned 404 for %s %s", tt.method, tt.path)
			}
		})
	}
}
