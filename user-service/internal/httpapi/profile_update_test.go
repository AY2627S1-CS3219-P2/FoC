// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded self-or-admin profile-update endpoint.
// Author review: PENDING — reviewer to complete
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

type fakeProfileUpdater struct{ uid uuid.UUID }

func (f *fakeProfileUpdater) UpdateProfile(_ context.Context, id uuid.UUID, _, _, _ string) (*user.User, error) {
	f.uid = id
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
