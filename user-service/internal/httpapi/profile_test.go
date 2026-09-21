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

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
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
		principal  handlers.Principal
		header     string
		getter     *fakeProfileGetter
		wantStatus int
		wantBody   []string
		avoidBody  []string
	}{
		"admin receives full profile": {
			principal: handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleAdmin}, header: "Bearer access-token", getter: &fakeProfileGetter{account: &user.User{UID: uid, Username: "student", Email: "student@u.nus.edu", PhoneNum: "+6512345678", AccountRole: user.AccountRoleStudent, AccountStatus: user.AccountStatusActive}}, wantStatus: http.StatusOK, wantBody: []string{`"email":"student@u.nus.edu"`, `"phone_num":"+6512345678"`, `"account_role":"STUDENT"`, `"account_status":"ACTIVE"`, `"date_created":"0001-01-01T00:00:00Z"`},
		},
		"non-admin receives restricted profile": {
			principal: handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleStudent}, header: "Bearer access-token", getter: &fakeProfileGetter{account: &user.User{UID: uid, Username: "student", Email: "student@u.nus.edu", PhoneNum: "+6512345678", AccountRole: user.AccountRoleStudent, AccountStatus: user.AccountStatusActive}}, wantStatus: http.StatusOK, wantBody: []string{`"uid":"` + uid.String() + `"`, `"username":"student"`, `"email":"student@u.nus.edu"`, `"phone_num":"+6512345678"`}, avoidBody: []string{`"account_role"`, `"account_status"`, `"date_created"`},
		},
		"rejects missing authentication": {getter: &fakeProfileGetter{}, wantStatus: http.StatusUnauthorized, wantBody: []string{"authentication required"}},
		"returns not found":              {principal: handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleStudent}, header: "Bearer access-token", getter: &fakeProfileGetter{err: user.ErrNotFound}, wantStatus: http.StatusNotFound, wantBody: []string{"user not found"}},
		"hides lookup failure":           {principal: handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleStudent}, header: "Bearer access-token", getter: &fakeProfileGetter{err: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError, wantBody: []string{"profile unavailable"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			verifier := &fakeTokenVerifier{principal: tt.principal}
			router := newTestRouter(routes.Dependencies{TokenVerifier: verifier, Profile: handlers.ProfileDependencies{ProfileGetter: tt.getter}})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+uid.String(), nil)
			request.Header.Set("Authorization", tt.header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			for _, expected := range tt.wantBody {
				if !strings.Contains(response.Body.String(), expected) {
					t.Fatalf("body = %q, want it to contain %q", response.Body.String(), expected)
				}
			}
			for _, forbidden := range tt.avoidBody {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("body = %q, must not contain %q", response.Body.String(), forbidden)
				}
			}
			if tt.wantStatus == http.StatusOK && tt.getter.uid != uid {
				t.Fatalf("GetByID uid = %s, want %s", tt.getter.uid, uid)
			}
		})
	}
}
