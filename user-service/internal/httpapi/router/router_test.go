// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added focused coverage for dedicated HTTP router setup.
// Author review: COMPLETED BY ZI YANG

package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"foc/user-service/internal/httpapi/routes"
)

func TestSetupMountsRoutes(t *testing.T) {
	handler := Setup(routes.Dependencies{})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if response.Code == http.StatusNotFound {
		t.Fatal("health route was not mounted")
	}
}
