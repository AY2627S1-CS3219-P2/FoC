// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused HTTP tests transcribed from the recorded JWKS OpenAPI contract.
// Author review: PENDING — reviewer to complete

package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
)

type fakeJWKSProvider struct {
	payload []byte
	err     error
}

func (f fakeJWKSProvider) JWKS() ([]byte, error) {
	return f.payload, f.err
}

func TestJWKSHandler(t *testing.T) {
	tests := map[string]struct {
		provider   handlers.JWKSProvider
		wantStatus int
		wantBody   string
	}{
		"returns the recorded public key set": {
			provider:   fakeJWKSProvider{payload: []byte(`{"keys":[{"kid":"active"}]}`)},
			wantStatus: http.StatusOK,
			wantBody:   `{"keys":[{"kid":"active"}]}`,
		},
		"reports provider failure": {
			provider:   fakeJWKSProvider{err: errors.New("key store unavailable")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "JWKS unavailable",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := newTestRouter(routes.Dependencies{System: handlers.SystemDependencies{JWKSProvider: tt.provider}})
			request := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", response.Body.String(), tt.wantBody)
			}
			if tt.wantStatus == http.StatusOK && response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
			}
		})
	}
}
