// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused HTTP tests for the recorded authenticated public-profile endpoint.
// Author review: PENDING — reviewer to complete

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type fakeProfileGetter struct {
	account *user.User
	err     error
	uid     uuid.UUID
}

func (f *fakeProfileGetter) GetByID(_ context.Context, uid uuid.UUID) (*user.User, error) {
	f.uid = uid
	return f.account, f.err
}

func TestProfileHandler(t *testing.T) {
	uid := uuid.New()
	tests := map[string]struct {
		header     string
		getter     *fakeProfileGetter
		wantStatus int
		wantBody   string
	}{
		"returns public profile": {
			header: "Bearer access-token", getter: &fakeProfileGetter{account: &user.User{UID: uid, Username: "student", AccountRole: user.AccountRoleStudent, AccountStatus: user.AccountStatusActive}}, wantStatus: http.StatusOK, wantBody: `"username":"student"`,
		},
		"rejects missing authentication": {getter: &fakeProfileGetter{}, wantStatus: http.StatusUnauthorized, wantBody: "authentication required"},
		"returns not found":              {header: "Bearer access-token", getter: &fakeProfileGetter{err: user.ErrNotFound}, wantStatus: http.StatusNotFound, wantBody: "user not found"},
		"hides lookup failure":           {header: "Bearer access-token", getter: &fakeProfileGetter{err: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError, wantBody: "profile unavailable"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			verifier := &fakeTokenVerifier{principal: Principal{UserID: uuid.New(), Role: user.AccountRoleStudent}}
			router := NewRouter(Dependencies{TokenVerifier: verifier, ProfileGetter: tt.getter})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+uid.String(), nil)
			request.Header.Set("Authorization", tt.header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", response.Body.String(), tt.wantBody)
			}
			if tt.wantStatus == http.StatusOK && tt.getter.uid != uid {
				t.Fatalf("GetByID uid = %s, want %s", tt.getter.uid, uid)
			}
		})
	}
}
