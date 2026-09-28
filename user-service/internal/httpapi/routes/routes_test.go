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

// AI-generated (edited by PENDING): exact statuses expose an incorrectly registered HTTP method as a test failure.
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
