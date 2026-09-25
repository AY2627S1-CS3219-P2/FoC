// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded self-or-admin profile-update endpoint.
// Author review: Validated tests reflects intended behaviour
package httpapi

import (
	"context"
	"foc/user-service/internal/user"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
)

type fakeProfileUpdater struct {
	uid   uuid.UUID
	phone string
	err   error
}

func (f *fakeProfileUpdater) UpdateProfile(_ context.Context, id uuid.UUID, _, phone, _ string) (*user.User, error) {
	f.uid = id
	f.phone = phone
	if f.err != nil {
		return nil, f.err
	}
	return &user.User{UID: id, Username: "new", AccountRole: user.AccountRoleStudent, AccountStatus: user.AccountStatusActive}, nil
}
func TestUpdateProfileHandler(t *testing.T) {
	id := uuid.New()
	for n, tt := range map[string]struct {
		principal handlers.Principal
		want      int
	}{"self": {handlers.Principal{UserID: id, Role: user.AccountRoleStudent}, 200}, "admin": {handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleAdmin}, 200}, "other": {handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleStudent}, 403}} {
		t.Run(n, func(t *testing.T) {
			u := &fakeProfileUpdater{}
			r := newTestRouter(routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: tt.principal}, Profile: handlers.ProfileDependencies{ProfileUpdater: u}})
			q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"username":"new"}`))
			q.Header.Set("Authorization", "Bearer token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, q)
			if w.Code != tt.want {
				t.Fatalf("status %d", w.Code)
			}
			if tt.want == 200 && u.uid != id {
				t.Fatal("not updated")
			}
		})
	}
}

func TestUpdateProfileHandlerPassesPhoneNumber(t *testing.T) {
	id := uuid.New()
	u := &fakeProfileUpdater{}
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: u},
	})
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"phone_num":"+6512345678"}`))
	q.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if u.phone != "+6512345678" {
		t.Fatalf("phone = %q, want %q", u.phone, "+6512345678")
	}
}

func TestUpdateProfileHandlerRejectsInvalidPassword(t *testing.T) {
	id := uuid.New()
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: &fakeProfileUpdater{err: user.ErrInvalidPassword}},
	})
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"password":"weakpass"}`))
	q.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "password must be 8-128 characters") {
		t.Fatalf("body = %q, want password validation error", w.Body.String())
	}
}

func TestUpdateProfileHandlerRejectsInvalidUsername(t *testing.T) {
	id := uuid.New()
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: &fakeProfileUpdater{err: user.ErrInvalidUsername}},
	})
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"username":"student_user"}`))
	q.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "username must be at most 128 characters") {
		t.Fatalf("body = %q, want username validation error", w.Body.String())
	}
}
