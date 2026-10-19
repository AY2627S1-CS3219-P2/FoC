// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added unit tests for JWT verification middleware and principal context.
// Author review: Validated tests reflects intended behaviour

package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type fakeTokenVerifier struct {
	principal handlers.Principal
	err       error
	token     string
}

type fakeRevocationChecker struct {
	revoked bool
	err     error
	calls   int
}

func (f *fakeRevocationChecker) Check(context.Context, handlers.Principal) (bool, error) {
	f.calls++
	return f.revoked, f.err
}

func (f *fakeTokenVerifier) Verify(_ context.Context, rawToken string) (handlers.Principal, error) {
	f.token = rawToken
	return f.principal, f.err
}

func TestRequireJWT(t *testing.T) {
	accountID := uuid.New()
	tests := map[string]struct {
		verifier     handlers.TokenVerifier
		authorize    string
		wantStatus   int
		wantHandler  bool
		wantVerifier bool
		wantToken    string
		checker      handlers.RevocationChecker
	}{
		"rejects missing authorization": {
			verifier:   &fakeTokenVerifier{},
			wantStatus: http.StatusUnauthorized,
		},
		"rejects non bearer authorization": {
			verifier:   &fakeTokenVerifier{},
			authorize:  "Basic credentials",
			wantStatus: http.StatusUnauthorized,
		},
		"rejects verifier failure": {
			verifier:     &fakeTokenVerifier{err: errors.New("invalid JWT")},
			authorize:    "Bearer invalid",
			wantStatus:   http.StatusUnauthorized,
			wantVerifier: true,
			wantToken:    "invalid",
		},
		"passes verified principal to handler": {
			verifier: &fakeTokenVerifier{principal: handlers.Principal{
				UserID: accountID,
				Role:   user.AccountRoleAdmin,
			}},
			authorize:    "Bearer signed.jwt",
			wantStatus:   http.StatusNoContent,
			wantHandler:  true,
			wantVerifier: true,
			wantToken:    "signed.jwt",
			checker:      &fakeRevocationChecker{},
		},
		"rejects revoked principal": {
			verifier:     &fakeTokenVerifier{principal: handlers.Principal{UserID: accountID}},
			authorize:    "Bearer revoked",
			checker:      &fakeRevocationChecker{revoked: true},
			wantStatus:   http.StatusUnauthorized,
			wantVerifier: true,
			wantToken:    "revoked",
		},
		"reports revocation failure": {
			verifier:     &fakeTokenVerifier{principal: handlers.Principal{UserID: accountID}},
			authorize:    "Bearer unavailable",
			checker:      &fakeRevocationChecker{err: errors.New("redis unavailable")},
			wantStatus:   http.StatusInternalServerError,
			wantVerifier: true,
			wantToken:    "unavailable",
		},
		"reports missing revocation checker": {
			verifier:     &fakeTokenVerifier{principal: handlers.Principal{UserID: accountID}},
			authorize:    "Bearer no-checker",
			wantStatus:   http.StatusInternalServerError,
			wantVerifier: true,
			wantToken:    "no-checker",
		},
		"reports missing verifier configuration": {
			wantStatus: http.StatusInternalServerError,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			verifier, _ := tt.verifier.(*fakeTokenVerifier)
			handlerCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				principal, ok := handlers.PrincipalFromContext(r.Context())
				if !ok {
					t.Fatal("principal missing from request context")
				}
				if principal.UserID != accountID || principal.Role != user.AccountRoleAdmin {
					t.Fatalf("principal = %#v, want admin account %s", principal, accountID)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", tt.authorize)
			response := httptest.NewRecorder()

			handlers.RequireJWT(tt.verifier, tt.checker, nil)(next).ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if handlerCalled != tt.wantHandler {
				t.Fatalf("handler called = %t, want %t", handlerCalled, tt.wantHandler)
			}
			if verifier != nil && verifier.token != tt.wantToken {
				t.Fatalf("verified token = %q, want %q", verifier.token, tt.wantToken)
			}
			if verifier != nil && (verifier.token != "") != tt.wantVerifier {
				t.Fatalf("verifier called = %t, want %t", verifier.token != "", tt.wantVerifier)
			}
		})
	}
}

func TestPrincipalFromContextWithoutPrincipal(t *testing.T) {
	if _, ok := handlers.PrincipalFromContext(context.Background()); ok {
		t.Fatal("principal found in empty context")
	}
}
