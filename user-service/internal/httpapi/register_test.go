// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused HTTP tests transcribed from the recorded registration OpenAPI contract.
// Author review: Validated tests

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/user"
)

type fakeRegistrar struct {
	err      error
	email    string
	username string
	password string
}

func (f *fakeRegistrar) Register(_ context.Context, email, username, password string) (*user.User, error) {
	f.email = email
	f.username = username
	f.password = password
	return &user.User{}, f.err
}

func TestRegisterHandler(t *testing.T) {
	tests := map[string]struct {
		body       string
		registrar  *fakeRegistrar
		wantStatus int
		wantError  string
	}{
		"creates account": {
			body:       `{"email":"student@u.nus.edu","username":"student","password":"ValidPass1"}`,
			registrar:  &fakeRegistrar{},
			wantStatus: http.StatusCreated,
		},
		"rejects malformed body": {
			body:       `{`,
			registrar:  &fakeRegistrar{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		"rejects incomplete credentials": {
			body:       `{"email":"student@u.nus.edu","username":"student","password":"short"}`,
			registrar:  &fakeRegistrar{},
			wantStatus: http.StatusBadRequest,
			wantError:  "8-character password",
		},
		// AI-generated (edited by ZI YANG).
		"rejects non-NUS email": {
			body:       `{"email":"student@example.com","username":"student","password":"ValidPass1"}`,
			registrar:  &fakeRegistrar{},
			wantStatus: http.StatusBadRequest,
			wantError:  "email must end with '@u.nus.edu'",
		},
		"reports duplicate email": {
			body:       `{"email":"student@u.nus.edu","username":"student","password":"ValidPass1"}`,
			registrar:  &fakeRegistrar{err: user.ErrDuplicateEmail},
			wantStatus: http.StatusConflict,
			wantError:  "account already exists",
		},
		"reports duplicate username": {
			body:       `{"email":"student@u.nus.edu","username":"student","password":"ValidPass1"}`,
			registrar:  &fakeRegistrar{err: user.ErrDuplicateUsername},
			wantStatus: http.StatusConflict,
			wantError:  "account already exists",
		},
		"hides registration failure": {
			body:       `{"email":"student@u.nus.edu","username":"student","password":"ValidPass1"}`,
			registrar:  &fakeRegistrar{err: errors.New("database unavailable")},
			wantStatus: http.StatusInternalServerError,
			wantError:  "registration unavailable",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := newTestRouter(routes.Dependencies{Auth: handlers.AuthDependencies{Registrar: tt.registrar}})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/users/register", strings.NewReader(tt.body))
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantError != "" && !strings.Contains(response.Body.String(), tt.wantError) {
				t.Fatalf("body = %q, want error containing %q", response.Body.String(), tt.wantError)
			}
			if tt.wantStatus == http.StatusCreated {
				if response.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", response.Body.String())
				}
				if tt.registrar.email != "student@u.nus.edu" || tt.registrar.username != "student" || tt.registrar.password != "ValidPass1" {
					t.Fatalf("registration input = %q/%q/%q", tt.registrar.email, tt.registrar.username, tt.registrar.password)
				}
			}
		})
	}
}
