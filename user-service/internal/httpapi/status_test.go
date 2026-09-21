// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded ADMIN-only account-status endpoint.
// Author review: PENDING — reviewer to complete

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"time"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type fakeStatusUpdater struct {
	uid    uuid.UUID
	status user.AccountStatus
	at     time.Time
}

func (f *fakeStatusUpdater) UpdateAccountStatus(_ context.Context, uid uuid.UUID, status user.AccountStatus, at time.Time) error {
	f.uid, f.status, f.at = uid, status, at
	return nil
}

func TestUpdateStatusHandler(t *testing.T) {
	uid := uuid.New()
	for name, tt := range map[string]struct {
		role user.AccountRole
		auth string
		want int
	}{
		"admin updates":     {user.AccountRoleAdmin, "Bearer token", http.StatusOK},
		"student forbidden": {user.AccountRoleStudent, "Bearer token", http.StatusForbidden},
		"missing auth":      {user.AccountRoleAdmin, "", http.StatusUnauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			updater := &fakeStatusUpdater{}
			router := newTestRouter(routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{Role: tt.role}}, Profile: handlers.ProfileDependencies{StatusUpdater: updater}})
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+uid.String()+"/status", strings.NewReader(`{"status":"SUSPENDED"}`))
			req.Header.Set("Authorization", tt.auth)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d", res.Code, tt.want)
			}
			if tt.want == http.StatusOK && (updater.uid != uid || updater.status != user.AccountStatusSuspended || updater.at.IsZero()) {
				t.Fatal("missing status update")
			}
		})
	}
}
