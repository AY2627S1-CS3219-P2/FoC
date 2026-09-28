// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded self-or-admin profile-update endpoint.
// Author review: Validated tests reflects intended behaviour
package handlers_test

import (
	"context"
	"foc/user-service/internal/user"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
)

type fakeProfileUpdater struct {
	uid          uuid.UUID
	phone        string
	current      string
	newPassword  string
	adminUpdated bool
	err          error
}

func (f *fakeProfileUpdater) UpdateProfile(_ context.Context, id uuid.UUID, _, phone, currentPassword, newPassword string) (*user.User, error) {
	f.uid = id
	f.phone = phone
	f.current = currentPassword
	f.newPassword = newPassword
	if f.err != nil {
		return nil, f.err
	}
	return &user.User{UID: id, Username: "new", AccountRole: user.AccountRoleStudent, AccountStatus: user.AccountStatusActive}, nil
}

func (f *fakeProfileUpdater) UpdateProfileAsAdmin(_ context.Context, id uuid.UUID, _, phone, newPassword string) (*user.User, error) {
	f.uid = id
	f.phone = phone
	f.newPassword = newPassword
	f.adminUpdated = true
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
			r := newTestRouter(routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: tt.principal}, Profile: handlers.ProfileDependencies{ProfileUpdater: u, AdminProfileUpdater: u}})
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
			if n == "admin" && !u.adminUpdated {
				t.Fatal("admin update did not use the admin path")
			}
		})
	}
}

func TestUpdateProfileHandlerPassesPhoneNumber(t *testing.T) {
	id := uuid.New()
	u := &fakeProfileUpdater{}
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: u, AdminProfileUpdater: u},
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
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"currentPassword":"OldPass1","newPassword":"weakpass"}`))
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

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — covers the recorded
// current-password failure response. Author review: PENDING.
func TestUpdateProfileHandlerRejectsIncorrectCurrentPassword(t *testing.T) {
	id := uuid.New()
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: &fakeProfileUpdater{err: user.ErrInvalidCurrentPassword}},
	})
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"currentPassword":"WrongPass1","newPassword":"NewPass1"}`))
	q.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, q)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid current password") {
		t.Fatalf("body = %q, want current-password error", w.Body.String())
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

// AI-generated (edited by ZI YANG): database-width validation must be reported as a client error.
func TestUpdateProfileHandlerRejectsPhoneLongerThanDatabaseWidth(t *testing.T) {
	id := uuid.New()
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: &fakeProfileUpdater{err: user.ErrInvalidPhone}},
	})
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"phone_num":"123456789012345678901"}`))
	q.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, q)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "phone number must be at most 20 characters") {
		t.Fatalf("body = %q, want phone-width error", w.Body.String())
	}
}

// AI-generated (edited by ZI YANG).
func TestUpdateProfileHandlerReturnsNotFoundForMissingAccount(t *testing.T) {
	id := uuid.New()
	r := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: id, Role: user.AccountRoleStudent}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: &fakeProfileUpdater{err: user.ErrNotFound}},
	})
	q := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+id.String(), strings.NewReader(`{"phone_num":"91234567"}`))
	q.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "user not found") {
		t.Fatalf("body = %q, want not-found error", w.Body.String())
	}
}
