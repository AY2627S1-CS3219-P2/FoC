// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded ADMIN-only account-status endpoint.
// Author review: Validated tests reflects intended behaviour

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
	"time"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type fakeStatusUpdater struct {
	uid    uuid.UUID
	status user.AccountStatus
	at     time.Time
	err    error
}

func (f *fakeStatusUpdater) UpdateAccountStatus(_ context.Context, uid uuid.UUID, status user.AccountStatus, at time.Time) error {
	f.uid, f.status, f.at = uid, status, at
	return f.err
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

func TestUpdateStatusHandlerCoversRecordedStatusTransitionsAndFailures(t *testing.T) {
	uid := uuid.New()
	tests := map[string]struct {
		body       string
		path       string
		updater    *fakeStatusUpdater
		withUpdate bool
		wantStatus int
		wantError  string
	}{
		"reactivates account": {
			body:       `{"status":"ACTIVE"}`,
			path:       "/api/v1/users/" + uid.String() + "/status",
			updater:    &fakeStatusUpdater{},
			withUpdate: true,
			wantStatus: http.StatusOK,
		},
		"rejects malformed body": {
			body:       "{",
			path:       "/api/v1/users/" + uid.String() + "/status",
			updater:    &fakeStatusUpdater{},
			withUpdate: true,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		"rejects invalid status": {
			body:       `{"status":"UNKNOWN"}`,
			path:       "/api/v1/users/" + uid.String() + "/status",
			updater:    &fakeStatusUpdater{},
			withUpdate: true,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid account status",
		},
		"rejects invalid user ID": {
			body:       `{"status":"SUSPENDED"}`,
			path:       "/api/v1/users/not-a-uuid/status",
			updater:    &fakeStatusUpdater{},
			withUpdate: true,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid user ID",
		},
		"reports missing updater": {
			body:       `{"status":"SUSPENDED"}`,
			path:       "/api/v1/users/" + uid.String() + "/status",
			wantStatus: http.StatusInternalServerError,
			wantError:  "account status unavailable",
		},
		"reports updater failure": {
			body:       `{"status":"SUSPENDED"}`,
			path:       "/api/v1/users/" + uid.String() + "/status",
			updater:    &fakeStatusUpdater{err: errors.New("database unavailable")},
			withUpdate: true,
			wantStatus: http.StatusInternalServerError,
			wantError:  "account status unavailable",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var statusUpdater handlers.AccountStatusUpdater
			if tt.updater != nil {
				statusUpdater = tt.updater
			}
			deps := routes.Dependencies{
				TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{Role: user.AccountRoleAdmin}},
				Profile:       handlers.ProfileDependencies{StatusUpdater: statusUpdater},
			}
			router := newTestRouter(deps)
			req := httptest.NewRequest(http.MethodPatch, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer token")
			res := httptest.NewRecorder()

			router.ServeHTTP(res, req)
			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tt.wantStatus, res.Body.String())
			}
			if tt.wantError != "" && !strings.Contains(res.Body.String(), tt.wantError) {
				t.Fatalf("body = %s, want error containing %q", res.Body.String(), tt.wantError)
			}
			if tt.withUpdate && tt.wantStatus == http.StatusOK && (tt.updater.uid != uid || tt.updater.status != user.AccountStatusActive || tt.updater.at.IsZero()) {
				t.Fatalf("status update = %#v, want active account update", tt.updater)
			}
		})
	}
}
