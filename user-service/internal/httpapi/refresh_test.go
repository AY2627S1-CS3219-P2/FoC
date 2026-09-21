// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused HTTP tests transcribed from the recorded refresh OpenAPI contract.
// Author review: PENDING — reviewer to complete

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
	"foc/user-service/internal/user"
)

type fakeRefresher struct {
	pair         user.TokenPair
	err          error
	refreshToken string
}

func (f *fakeRefresher) Refresh(_ context.Context, refreshToken string) (user.TokenPair, error) {
	f.refreshToken = refreshToken
	return f.pair, f.err
}

func TestRefreshHandler(t *testing.T) {
	tests := map[string]struct {
		body       string
		refresher  *fakeRefresher
		wantStatus int
		wantError  string
	}{
		"returns rotated token pair": {
			body:       `{"refreshToken":"old-refresh"}`,
			refresher:  &fakeRefresher{pair: user.TokenPair{AccessToken: "new-access", RefreshToken: "new-refresh"}},
			wantStatus: http.StatusOK,
		},
		"rejects malformed JSON": {
			body:       `{`,
			refresher:  &fakeRefresher{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		"rejects missing refresh token": {
			body:       `{}`,
			refresher:  &fakeRefresher{},
			wantStatus: http.StatusBadRequest,
			wantError:  "refreshToken is required",
		},
		"reports replay with recorded error": {
			body:       `{"refreshToken":"replayed"}`,
			refresher:  &fakeRefresher{err: user.ErrSessionCompromised},
			wantStatus: http.StatusUnauthorized,
			wantError:  "ErrSessionCompromised",
		},
		"rejects invalid refresh token": {
			body:       `{"refreshToken":"invalid"}`,
			refresher:  &fakeRefresher{err: user.ErrSessionNotFound},
			wantStatus: http.StatusUnauthorized,
			wantError:  "authentication required",
		},
		"rejects verifier failure": {
			body:       `{"refreshToken":"invalid"}`,
			refresher:  &fakeRefresher{err: errors.New("JWT invalid")},
			wantStatus: http.StatusUnauthorized,
			wantError:  "authentication required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := newTestRouter(routes.Dependencies{Auth: handlers.AuthDependencies{Refresher: tt.refresher}})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/users/refresh", strings.NewReader(tt.body))
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantError != "" && !strings.Contains(response.Body.String(), tt.wantError) {
				t.Fatalf("body = %s, want error containing %q", response.Body.String(), tt.wantError)
			}
			if tt.wantStatus == http.StatusOK {
				var got handlers.AuthResponse
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got != (handlers.AuthResponse{AccessToken: "new-access", RefreshToken: "new-refresh"}) {
					t.Fatalf("response = %#v", got)
				}
				if tt.refresher.refreshToken != "old-refresh" {
					t.Fatalf("refresh token = %q, want old-refresh", tt.refresher.refreshToken)
				}
			}
		})
	}
}
