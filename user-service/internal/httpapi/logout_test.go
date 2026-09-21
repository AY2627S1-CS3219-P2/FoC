// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused HTTP tests transcribed from the recorded logout OpenAPI contract.
// Author review: PENDING — reviewer to complete

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
	"foc/user-service/internal/session"
	"time"

	"github.com/google/uuid"
)

type fakeAccessVerifier struct {
	claims session.AccessTokenClaims
	err    error
	raw    string
}

func (f *fakeAccessVerifier) VerifyAccess(_ context.Context, rawToken string) (session.AccessTokenClaims, error) {
	f.raw = rawToken
	return f.claims, f.err
}

type fakeLogoutter struct {
	err          error
	accessClaims session.AccessTokenClaims
	refreshToken string
}

func (f *fakeLogoutter) Logout(_ context.Context, access session.AccessTokenClaims, refreshToken string) error {
	f.accessClaims = access
	f.refreshToken = refreshToken
	return f.err
}

func TestLogoutHandler(t *testing.T) {
	claims := session.AccessTokenClaims{JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
	tests := map[string]struct {
		header     string
		body       string
		verifier   *fakeAccessVerifier
		logoutter  *fakeLogoutter
		wantStatus int
		wantError  string
	}{
		"logs out verified session": {
			header:     "Bearer access-token",
			body:       `{"refreshToken":"refresh-token"}`,
			verifier:   &fakeAccessVerifier{claims: claims},
			logoutter:  &fakeLogoutter{},
			wantStatus: http.StatusNoContent,
		},
		"rejects missing access token": {
			body:       `{"refreshToken":"refresh-token"}`,
			verifier:   &fakeAccessVerifier{claims: claims},
			logoutter:  &fakeLogoutter{},
			wantStatus: http.StatusUnauthorized,
			wantError:  "authentication required",
		},
		"rejects malformed access token": {
			header:     "Basic access-token",
			body:       `{"refreshToken":"refresh-token"}`,
			verifier:   &fakeAccessVerifier{claims: claims},
			logoutter:  &fakeLogoutter{},
			wantStatus: http.StatusUnauthorized,
			wantError:  "authentication required",
		},
		"rejects invalid access token": {
			header:     "Bearer invalid",
			body:       `{"refreshToken":"refresh-token"}`,
			verifier:   &fakeAccessVerifier{err: errors.New("invalid JWT")},
			logoutter:  &fakeLogoutter{},
			wantStatus: http.StatusUnauthorized,
			wantError:  "authentication required",
		},
		"rejects malformed body": {
			header:     "Bearer access-token",
			body:       `{`,
			verifier:   &fakeAccessVerifier{claims: claims},
			logoutter:  &fakeLogoutter{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		"reports invalidation failure": {
			header:     "Bearer access-token",
			body:       `{"refreshToken":"refresh-token"}`,
			verifier:   &fakeAccessVerifier{claims: claims},
			logoutter:  &fakeLogoutter{err: errors.New("Redis unavailable")},
			wantStatus: http.StatusInternalServerError,
			wantError:  "logout failed",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := newTestRouter(routes.Dependencies{Auth: handlers.AuthDependencies{AccessVerifier: tt.verifier, Logoutter: tt.logoutter}})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/users/logout", strings.NewReader(tt.body))
			request.Header.Set("Authorization", tt.header)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantError != "" && !strings.Contains(response.Body.String(), tt.wantError) {
				t.Fatalf("body = %s, want error containing %q", response.Body.String(), tt.wantError)
			}
			if tt.wantStatus == http.StatusNoContent {
				if response.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", response.Body.String())
				}
				if tt.verifier.raw != "access-token" || tt.logoutter.accessClaims != claims || tt.logoutter.refreshToken != "refresh-token" {
					t.Fatalf("logout inputs = raw=%q claims=%#v refresh=%q", tt.verifier.raw, tt.logoutter.accessClaims, tt.logoutter.refreshToken)
				}
			}
		})
	}
}
