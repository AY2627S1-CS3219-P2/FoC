// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: HTTP tests for the admin dashboard's account list and audit log
//   (D-110, D-121 to D-123), and for who an admin status change is attributed to
//   and the 403 an admin gets for suspending an admin or themselves (D-118,
//   D-119).
// Author review: PENDING — <reviewer to complete>

package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

type fakeSearcher struct {
	got  user.UserFilter
	page *user.UserPage
	err  error
}

func (f *fakeSearcher) SearchUsers(_ context.Context, flt user.UserFilter) (*user.UserPage, error) {
	f.got = flt
	if f.err != nil {
		return nil, f.err
	}
	if f.page != nil {
		return f.page, nil
	}
	return &user.UserPage{Items: []*user.User{}, Limit: 50}, nil
}

type fakeAuditReader struct {
	got  user.AuditFilter
	page *user.AuditPage
	err  error
}

func (f *fakeAuditReader) AuditLog(_ context.Context, flt user.AuditFilter) (*user.AuditPage, error) {
	f.got = flt
	if f.err != nil {
		return nil, f.err
	}
	if f.page != nil {
		return f.page, nil
	}
	return &user.AuditPage{Items: []*user.AuditEntry{}, Limit: 50}, nil
}

func adminRouter(role user.AccountRole, s *fakeSearcher, a *fakeAuditReader) http.Handler {
	return newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: uuid.New(), Role: role}},
		Admin:         handlers.AdminDependencies{Searcher: s, Audit: a},
	})
}

func get(h http.Handler, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheAdminListAndAuditLogAreAdminOnly(t *testing.T) {
	for _, path := range []string{"/api/v1/users", "/api/v1/users/audit-log"} {
		for name, tc := range map[string]struct {
			role user.AccountRole
			auth string
			want int
		}{
			"admin":   {user.AccountRoleAdmin, "Bearer token", http.StatusOK},
			"student": {user.AccountRoleStudent, "Bearer token", http.StatusForbidden},
			"no auth": {user.AccountRoleAdmin, "", http.StatusUnauthorized},
		} {
			t.Run(path+" "+name, func(t *testing.T) {
				rec := get(adminRouter(tc.role, &fakeSearcher{}, &fakeAuditReader{}), path, tc.auth)
				if rec.Code != tc.want {
					t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
				}
			})
		}
	}
}

func TestTheLiteralAdminPathsDoNotShadowAProfileLookup(t *testing.T) {
	rec := get(adminRouter(user.AccountRoleAdmin, &fakeSearcher{}, &fakeAuditReader{}), "/api/v1/users/"+uuid.NewString(), "Bearer token")
	// No ProfileGetter is wired, so this is the profile handler's 500, not the list's 200.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d: a uid path must still reach the profile handler", rec.Code)
	}
}

func TestListUsersPassesTheFilterAndShowsEveryRowField(t *testing.T) {
	last := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	uid := uuid.New()
	searcher := &fakeSearcher{page: &user.UserPage{
		Total: 1, Limit: 50,
		Items: []*user.User{{UID: uid, Username: "alice", Email: "a@u.nus.edu", PhoneNum: "9123", AccountRole: user.AccountRoleStudent,
			AccountStatus: user.AccountStatusSuspended, DateCreated: last.Add(-time.Hour), LastLoginDate: &last}},
	}}

	rec := get(adminRouter(user.AccountRoleAdmin, searcher, &fakeAuditReader{}), "/api/v1/users?q=ali&status=SUSPENDED&limit=20&offset=40", "Bearer token")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if searcher.got.Query != "ali" || searcher.got.Status != user.AccountStatusSuspended || searcher.got.Limit != 20 || searcher.got.Offset != 40 {
		t.Errorf("filter = %+v", searcher.got)
	}
	var page struct {
		Items  []map[string]any `json:"items"`
		Total  int              `json:"total"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	for _, f := range []string{"uid", "username", "email", "phone_num", "account_role", "account_status", "date_created", "last_login_date"} {
		if _, ok := page.Items[0][f]; !ok {
			t.Errorf("a row is missing %q (D-122)", f)
		}
	}
	if _, leaked := page.Items[0]["password"]; leaked {
		t.Error("a password hash must never be listed")
	}
}

func TestListUsersRejectsBadInputAndMapsTheServiceRefusal(t *testing.T) {
	h := adminRouter(user.AccountRoleAdmin, &fakeSearcher{}, &fakeAuditReader{})
	for _, q := range []string{"limit=ten", "offset=x"} {
		if rec := get(h, "/api/v1/users?"+q, "Bearer token"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", q, rec.Code)
		}
	}
	refusing := adminRouter(user.AccountRoleAdmin, &fakeSearcher{err: user.ErrInvalidFilter}, &fakeAuditReader{})
	if rec := get(refusing, "/api/v1/users?status=BANNED", "Bearer token"); rec.Code != http.StatusBadRequest {
		t.Errorf("a refused filter is 400, got %d", rec.Code)
	}
	broken := adminRouter(user.AccountRoleAdmin, &fakeSearcher{err: errors.New("database down")}, &fakeAuditReader{})
	rec := get(broken, "/api/v1/users", "Bearer token")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "database down") {
		t.Errorf("a failure is a sanitized 500, got %d %s", rec.Code, rec.Body)
	}
}

func TestAuditLogPassesItsFiltersAndShowsTheRecord(t *testing.T) {
	reader := &fakeAuditReader{page: &user.AuditPage{Total: 1, Limit: 50, Items: []*user.AuditEntry{{
		ID: "1", OccurredAt: time.Now(), ActorID: "a", ActorRole: "ADMIN", Action: user.ActionSuspend, TargetType: user.AuditTargetType,
		TargetID: "t", Outcome: user.OutcomeRefused, RefusalReason: "an admin account cannot be suspended",
		BeforeState: map[string]any{"account_status": "ACTIVE"}, CorrelationID: "req",
	}}}}

	rec := get(adminRouter(user.AccountRoleAdmin, &fakeSearcher{}, reader),
		"/api/v1/users/audit-log?action=account.suspend&outcome=refused&actor_id=a&target_id=t&from=2026-11-01&to=2026-11-30T00:00:00Z&limit=10&offset=5", "Bearer token")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	g := reader.got
	if g.Action != "account.suspend" || g.Outcome != "refused" || g.ActorID != "a" || g.TargetID != "t" || g.Limit != 10 || g.Offset != 5 {
		t.Errorf("filter = %+v", g)
	}
	if g.From == nil || g.From.Format("2006-01-02") != "2026-11-01" || g.To == nil || g.To.Format("2006-01-02") != "2026-11-30" {
		t.Errorf("times = %v %v", g.From, g.To)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	for _, f := range []string{"id", "occurred_at", "actor_id", "actor_role", "action", "target_type", "target_id", "outcome",
		"refusal_reason", "reason", "before_state", "after_state", "additional_comments", "correlation_id"} {
		if _, ok := page.Items[0][f]; !ok {
			t.Errorf("a row is missing %q (D-113)", f)
		}
	}
}

func TestAuditLogRejectsBadParameters(t *testing.T) {
	h := adminRouter(user.AccountRoleAdmin, &fakeSearcher{}, &fakeAuditReader{})
	for _, q := range []string{"limit=ten", "offset=x", "from=yesterday", "to=2026-13-45"} {
		if rec := get(h, "/api/v1/users/audit-log?"+q, "Bearer token"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", q, rec.Code)
		}
	}
	refusing := adminRouter(user.AccountRoleAdmin, &fakeSearcher{}, &fakeAuditReader{err: user.ErrInvalidFilter})
	if rec := get(refusing, "/api/v1/users/audit-log?outcome=maybe", "Bearer token"); rec.Code != http.StatusBadRequest {
		t.Errorf("a refused filter is 400, got %d", rec.Code)
	}
}

func TestAnAdminStatusChangeIsAttributedToTheVerifiedPrincipalNotAHeader(t *testing.T) {
	adminID, target := uuid.New(), uuid.New()
	updater := &fakeStatusUpdater{}
	router := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: adminID, Role: user.AccountRoleAdmin}},
		Profile:       handlers.ProfileDependencies{StatusUpdater: updater},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+target.String()+"/status", strings.NewReader(`{"status":"SUSPENDED"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-User-ID", uuid.NewString())
	req.Header.Set("X-User-Role", "STUDENT")
	req.Header.Set("X-Request-Id", "req-xyz")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if updater.audit.ActorID != adminID.String() || updater.audit.ActorRole != "ADMIN" {
		t.Errorf("attributed to %+v, want the verified principal (D-041)", updater.audit)
	}
	if updater.audit.CorrelationID == "" {
		t.Error("the audit context carries the request id")
	}
}

func TestSuspendingAnAdminOrOneselfIs403(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{user.ErrCannotSuspendAdmin, "an admin account cannot be suspended"},
		{user.ErrCannotSuspendSelf, "you cannot suspend your own account"},
	} {
		updater := &fakeStatusUpdater{err: tc.err}
		router := newTestRouter(routes.Dependencies{
			TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: uuid.New(), Role: user.AccountRoleAdmin}},
			Profile:       handlers.ProfileDependencies{StatusUpdater: updater},
		})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+uuid.NewString()+"/status", strings.NewReader(`{"status":"SUSPENDED"}`))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%v: got %d %s", tc.err, rec.Code, rec.Body)
		}
	}
}

func TestAnAdminEditOfAnotherAccountIsAttributedToThem(t *testing.T) {
	adminID, target := uuid.New(), uuid.New()
	updater := &fakeProfileUpdater{}
	router := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: adminID, Role: user.AccountRoleAdmin}},
		Profile:       handlers.ProfileDependencies{ProfileUpdater: updater, AdminProfileUpdater: updater},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+target.String(), strings.NewReader(`{"newPassword":"BrandNewPass9"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !updater.adminUpdated {
		t.Fatalf("status = %d adminUpdated = %v", rec.Code, updater.adminUpdated)
	}
	if updater.audit.ActorID != adminID.String() {
		t.Errorf("attributed to %+v", updater.audit)
	}
}
