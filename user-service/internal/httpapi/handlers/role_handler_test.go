// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Role endpoint authentication, refusal mapping, body limit and privacy.
// Author review: ZI YANG - reviewed and verified

package handlers_test

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

type fakeRoleUpdater struct {
	err    error
	actor  user.AuditContext
	target uuid.UUID
	role   user.AccountRole
	called bool
}

func (f *fakeRoleUpdater) AdminUpdateRole(_ context.Context, ac user.AuditContext, id uuid.UUID, role user.AccountRole) error {
	f.called = true
	f.actor = ac
	f.target = id
	f.role = role
	return f.err
}
func TestRoleEndpoint(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		name, body, auth, path string
		principal              user.AccountRole
		err                    error
		want                   int
	}{
		{"success", `{"role":"ADMIN"}`, "Bearer token", id.String(), user.AccountRoleAdmin, nil, 204},
		{"no auth", `{"role":"ADMIN"}`, "", id.String(), user.AccountRoleAdmin, nil, 401},
		{"student caller", `{"role":"ADMIN"}`, "Bearer token", id.String(), user.AccountRoleStudent, nil, 403},
		{"invalid ID", `{"role":"ADMIN"}`, "Bearer token", "bad", user.AccountRoleAdmin, nil, 400},
		{"trailing JSON", `{"role":"ADMIN"}{}`, "Bearer token", id.String(), user.AccountRoleAdmin, nil, 400},
		{"trailing garbage", `{"role":"ADMIN"}garbage`, "Bearer token", id.String(), user.AccountRoleAdmin, nil, 400},
		{"oversized whitespace suffix", `{"role":"ADMIN"}` + strings.Repeat(" ", 11*1024), "Bearer token", id.String(), user.AccountRoleAdmin, nil, 413},
		{"invalid JSON", `{`, "Bearer token", id.String(), user.AccountRoleAdmin, nil, 400},
		{"invalid role", `{"role":"COURIER"}`, "Bearer token", id.String(), user.AccountRoleAdmin, user.ErrInvalidRole, 400},
		{"self demotion", `{"role":"STUDENT"}`, "Bearer token", id.String(), user.AccountRoleAdmin, user.ErrCannotDemoteSelf, 403},
		{"stale admin authority", `{"role":"STUDENT"}`, "Bearer token", id.String(), user.AccountRoleAdmin, user.ErrAdminRequired, 403},
		{"missing account", `{"role":"ADMIN"}`, "Bearer token", id.String(), user.AccountRoleAdmin, user.ErrNotFound, 404},
		{"last active admin", `{"role":"STUDENT"}`, "Bearer token", id.String(), user.AccountRoleAdmin, user.ErrLastActiveAdmin, 409},
		{"suspended student", `{"role":"ADMIN"}`, "Bearer token", id.String(), user.AccountRoleAdmin, user.ErrInactivePromotion, 409},
		{"infrastructure failure", `{"role":"ADMIN"}`, "Bearer token", id.String(), user.AccountRoleAdmin, errors.New("private database details"), 500},
		{"oversized body", `{"role":"` + strings.Repeat("A", 11*1024) + `"}`, "Bearer token", id.String(), user.AccountRoleAdmin, nil, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updater := &fakeRoleUpdater{err: tc.err}
			actor := uuid.New()
			handler := newTestRouter(routes.Dependencies{TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: actor, Role: tc.principal}}, Admin: handlers.AdminDependencies{RoleUpdater: updater}})
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+tc.path+"/role", strings.NewReader(tc.body))
			req.Header.Set("Authorization", tc.auth)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if rec.Code == 204 && (rec.Body.Len() != 0 || !updater.called || updater.actor.ActorID != actor.String() || updater.target != id || updater.role != user.AccountRoleAdmin) {
				t.Fatalf("successful change did not use verified identity: %+v", updater)
			}
			if strings.Contains(rec.Body.String(), "private database") {
				t.Fatal("internal details exposed")
			}
		})
	}
}
