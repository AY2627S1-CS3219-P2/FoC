// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added route and handler tests for the user-service HTTP scaffold.
// Author review: Verified correctness

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type fakeLoginService struct {
	pair       session.TokenPair
	err        error
	identifier string
	password   string
}

func (f *fakeLoginService) Login(_ context.Context, identifier, password string) (session.TokenPair, error) {
	f.identifier = identifier
	f.password = password
	return f.pair, f.err
}

func TestNewRouterRegistersRecordedRoutes(t *testing.T) {
	router := newTestRouter(routes.Dependencies{
		Auth:   handlers.AuthDependencies{LoginService: &fakeLoginService{}},
		System: handlers.SystemDependencies{HealthCheck: func(context.Context) error { return nil }},
	})

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
			req := httptest.NewRequest(tt.method, tt.path, nil)
			res := httptest.NewRecorder()

			router.ServeHTTP(res, req)

			if res.Code == http.StatusNotFound {
				t.Fatalf("route returned 404 for %s %s", tt.method, tt.path)
			}
		})
	}
}

func TestLoginHandler(t *testing.T) {
	tests := map[string]struct {
		body       string
		authPair   session.TokenPair
		authErr    error
		wantStatus int
		wantError  string
	}{
		"returns JWT pair": {
			body:       `{"identifier":"student","password":"ValidPass1"}`,
			authPair:   session.TokenPair{AccessToken: "access", RefreshToken: "refresh"},
			wantStatus: http.StatusOK,
		},
		"rejects malformed JSON": {
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		"rejects missing credentials": {
			body:       `{"identifier":"student"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "identifier and password are required",
		},
		"returns generic invalid credentials response": {
			body:       `{"identifier":"student","password":"wrong"}`,
			authErr:    user.ErrInvalidCredentials,
			wantStatus: http.StatusUnauthorized,
			wantError:  "invalid credentials",
		},
		"reports suspended account": {
			body:       `{"identifier":"student","password":"ValidPass1"}`,
			authErr:    user.ErrAccountSuspended,
			wantStatus: http.StatusForbidden,
			wantError:  "account suspended",
		},
		"hides internal authentication failures": {
			body:       `{"identifier":"student","password":"ValidPass1"}`,
			authErr:    errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantError:  "authentication unavailable",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			loginService := &fakeLoginService{pair: tt.authPair, err: tt.authErr}
			router := newTestRouter(routes.Dependencies{
				Auth:   handlers.AuthDependencies{LoginService: loginService},
				System: handlers.SystemDependencies{HealthCheck: func(context.Context) error { return nil }},
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/users/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			router.ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tt.wantStatus, res.Body.String())
			}
			if tt.wantError != "" && !strings.Contains(res.Body.String(), tt.wantError) {
				t.Fatalf("body = %s, want error containing %q", res.Body.String(), tt.wantError)
			}
			if tt.wantStatus == http.StatusOK {
				var got handlers.AuthResponse
				if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if got != (handlers.AuthResponse{AccessToken: "access", RefreshToken: "refresh"}) {
					t.Fatalf("response = %#v, want JWT pair", got)
				}
				if loginService.identifier != "student" || loginService.password != "ValidPass1" {
					t.Fatalf("credentials = %q/%q, want request values", loginService.identifier, loginService.password)
				}
			}
		})
	}
}

func TestLoginHandlerDoesNotApplyNewPasswordPolicy(t *testing.T) {
	loginService := &fakeLoginService{pair: session.TokenPair{AccessToken: "access", RefreshToken: "refresh"}}
	router := newTestRouter(routes.Dependencies{
		Auth:   handlers.AuthDependencies{LoginService: loginService},
		System: handlers.SystemDependencies{HealthCheck: func(context.Context) error { return nil }},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/login", strings.NewReader(`{"identifier":"student","password":"weakpass"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if loginService.password != "weakpass" {
		t.Fatalf("login password = %q, want weakpass passed through for authentication", loginService.password)
	}
}

func TestHealthHandler(t *testing.T) {
	tests := map[string]struct {
		healthErr  error
		wantStatus int
		wantBody   string
	}{
		"healthy":   {wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		"unhealthy": {healthErr: errors.New("database unavailable"), wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unhealthy"}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := newTestRouter(routes.Dependencies{
				Auth:   handlers.AuthDependencies{LoginService: &fakeLoginService{}},
				System: handlers.SystemDependencies{HealthCheck: func(context.Context) error { return tt.healthErr }},
			})
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tt.wantStatus)
			}
			if strings.TrimSpace(res.Body.String()) != tt.wantBody {
				t.Fatalf("body = %q, want %q", res.Body.String(), tt.wantBody)
			}
		})
	}
}
