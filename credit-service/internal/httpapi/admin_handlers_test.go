// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Tests of credit-service's admin routes (D-150 to D-153) through the
//   router: only an ADMIN token passes, each Re-verify outcome answers as it
//   should and is logged (or not) as D-152 says, and bad input is a 400.
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/gen"
	"foc/credit-service/internal/httpapi"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAdminRepo is an in-memory credit.AdminRepository: the tests set what
// Re-verify answers and read back the audit rows the service wrote.
type fakeAdminRepo struct {
	mu        sync.Mutex
	flagged   []credit.FlaggedAccount
	account   *credit.Account
	accountEr error
	result    *credit.ReverifyResult
	reverEr   error
	recorded  []credit.AuditEntry
	filter    credit.AuditFilter
	recordEr  error
}

func (f *fakeAdminRepo) ListFlaggedAccounts(_ context.Context, limit, offset int) (*credit.FlaggedPage, error) {
	return &credit.FlaggedPage{Items: f.flagged, Total: len(f.flagged), Limit: limit, Offset: offset}, nil
}

func (f *fakeAdminRepo) GetAdminAccount(context.Context, uuid.UUID) (*credit.Account, error) {
	return f.account, f.accountEr
}

func (f *fakeAdminRepo) ReverifyAccount(context.Context, uuid.UUID, *credit.AuditEntry) (*credit.ReverifyResult, error) {
	return f.result, f.reverEr
}

func (f *fakeAdminRepo) RecordAudit(_ context.Context, e *credit.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordEr != nil {
		return f.recordEr
	}
	f.recorded = append(f.recorded, *e)
	return nil
}

func (f *fakeAdminRepo) ListAudit(_ context.Context, filter credit.AuditFilter) (*credit.AuditPage, error) {
	f.filter = filter
	return &credit.AuditPage{Items: []*credit.AuditEntry{{
		ID: uuid.NewString(), OccurredAt: time.Now().UTC(), ActorID: uuid.NewString(), ActorRole: "ADMIN",
		Action: credit.AuditReverify, TargetType: credit.AuditTargetType, TargetID: "t",
		Outcome: credit.OutcomeSucceeded, BeforeState: map[string]any{"integrity_status": "FLAGGED"},
	}}, Total: 1, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (f *fakeAdminRepo) PurgeAudit(context.Context) (int64, error) { return 0, nil }

// adminRig is a router over fakeAdminRepo with tokens for an admin and a student.
type adminRig struct {
	router  http.Handler
	repo    *fakeAdminRepo
	admin   string
	student string
	adminID uuid.UUID
}

func newAdminRig(t *testing.T) *adminRig {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)
	repo := &fakeAdminRepo{}
	svc := credit.NewService(nil, credit.WithAdminRepository(repo))
	router := httpapi.NewRouter(nil, auth.NewVerifier(jwks.URL, nil), nil, svc, nil)
	adminID := uuid.New()
	return &adminRig{
		router:  router,
		repo:    repo,
		admin:   iss.mint(t, tokenOpts{subject: adminID.String(), role: "ADMIN"}),
		student: iss.mint(t, tokenOpts{subject: uuid.NewString(), role: "STUDENT"}),
		adminID: adminID,
	}
}

func (a *adminRig) do(method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

const adminBase = "/api/v1/credits/admin"

func TestAdminRoutesAreForAdminsOnly(t *testing.T) {
	a := newAdminRig(t)
	uid := uuid.NewString()
	routes := []struct{ method, path string }{
		{http.MethodGet, adminBase + "/flagged-accounts"},
		{http.MethodGet, adminBase + "/users/" + uid + "/account"},
		{http.MethodPost, adminBase + "/users/" + uid + "/reconcile"},
		{http.MethodGet, adminBase + "/audit-log"},
	}
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, a.do(r.method, r.path, "").Code, "no token")
			rec := a.do(r.method, r.path, a.student)
			assert.Equal(t, http.StatusForbidden, rec.Code, "a student's token")
			var body gen.ErrorResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
			assert.Equal(t, gen.FORBIDDEN, body.Code)
		})
	}
	assert.Empty(t, a.repo.recorded, "a refused caller leaves no audit row: it is not an admin action")
}

func TestListFlaggedAccounts(t *testing.T) {
	a := newAdminRig(t)
	id, detail, expected, observed := uuid.New(), "ledger available sum differs", 100.0, 105.0
	a.repo.flagged = []credit.FlaggedAccount{{
		UserID: id, ReasonCode: "AVAILABLE_PROJECTION_MISMATCH", Detail: &detail,
		Expected: &expected, Observed: &observed, FlaggedAt: time.Now().UTC(),
	}}

	rec := a.do(http.MethodGet, adminBase+"/flagged-accounts?limit=500", a.admin)
	require.Equal(t, http.StatusOK, rec.Code)
	var page gen.FlaggedAccountsPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	assert.Equal(t, 1, page.Total)
	assert.Equal(t, credit.MaxAdminPage, page.Limit, "an oversized limit is cut to one page (D-104)")
	require.Len(t, page.Items, 1)
	assert.Equal(t, id, page.Items[0].UserId)
	assert.Equal(t, "AVAILABLE_PROJECTION_MISMATCH", page.Items[0].ReasonCode)
	assert.InDelta(t, 105, *page.Items[0].Observed, 0)

	assert.Equal(t, http.StatusBadRequest, a.do(http.MethodGet, adminBase+"/flagged-accounts?offset=-1", a.admin).Code)
}

func TestGetAccountForAdmin(t *testing.T) {
	a := newAdminRig(t)
	id, code := uuid.New(), "OUT_OF_BOUNDS_BALANCE"
	now := time.Now().UTC()
	a.repo.account = &credit.Account{
		UserID: id, AvailableCredits: 90, ReservedCredits: 10, IntegrityStatus: credit.IntegrityFlagged,
		IntegrityFlaggedAt: &now, IntegrityReasonCode: &code, CreatedAt: now, UpdatedAt: now,
	}

	rec := a.do(http.MethodGet, adminBase+"/users/"+id.String()+"/account", a.admin)
	require.Equal(t, http.StatusOK, rec.Code)
	var got gen.AdminAccountResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, int64(90), got.AvailableCredits, "the admin sees the real balance of a flagged account")
	assert.Equal(t, gen.IntegrityStatus("FLAGGED"), got.IntegrityStatus)
	assert.Equal(t, code, *got.ReasonCode)

	a.repo.account, a.repo.accountEr = nil, credit.ErrWalletNotReady
	assert.Equal(t, http.StatusNotFound, a.do(http.MethodGet, adminBase+"/users/"+id.String()+"/account", a.admin).Code)
	assert.Equal(t, http.StatusBadRequest, a.do(http.MethodGet, adminBase+"/users/not-a-uuid/account", a.admin).Code)
}

func TestReverifyOutcomesAndTheirAuditRows(t *testing.T) {
	uid := uuid.New()
	path := adminBase + "/users/" + uid.String() + "/reconcile"
	expected, observed := 100.0, 105.0

	cases := []struct {
		name       string
		result     *credit.ReverifyResult
		err        error
		status     int
		outcome    string
		wantRows   int
		wantStatus string // the audit row's outcome, when one is written
		wantReason string
	}{
		{name: "cleared", result: &credit.ReverifyResult{Outcome: credit.ReverifyCleared}, status: 200, outcome: "CLEARED", wantRows: 0},
		{name: "still flagged", result: &credit.ReverifyResult{Outcome: credit.ReverifyStillFlagged, Finding: &credit.Finding{
			ReasonCode: "AVAILABLE_PROJECTION_MISMATCH", Detail: "ledger available sum differs", Expected: &expected, Observed: &observed}},
			status: 200, outcome: "STILL_FLAGGED", wantRows: 1, wantStatus: credit.OutcomeRefused, wantReason: "AVAILABLE_PROJECTION_MISMATCH"},
		{name: "stale", result: &credit.ReverifyResult{Outcome: credit.ReverifyStale}, status: 200, outcome: "STALE",
			wantRows: 1, wantStatus: credit.OutcomeRefused, wantReason: "changed while"},
		{name: "not flagged", result: &credit.ReverifyResult{Outcome: credit.ReverifyNotFlagged}, status: 200, outcome: "NOT_FLAGGED", wantRows: 0},
		{name: "no account", err: credit.ErrWalletNotReady, status: 404, wantRows: 1, wantStatus: credit.OutcomeRefused, wantReason: "no credit account"},
		{name: "a database failure", err: errors.New("connection reset"), status: 500, wantRows: 1, wantStatus: credit.OutcomeFailed, wantReason: "connection reset"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newAdminRig(t)
			a.repo.result, a.repo.reverEr = c.result, c.err

			rec := a.do(http.MethodPost, path, a.admin)
			require.Equal(t, c.status, rec.Code, rec.Body.String())
			if c.outcome != "" {
				var body gen.ReverifyResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				assert.Equal(t, gen.ReverifyOutcome(c.outcome), body.Outcome)
				if c.outcome == "STILL_FLAGGED" {
					assert.Equal(t, "AVAILABLE_PROJECTION_MISMATCH", *body.ReasonCode, "the current finding is returned (D-153)")
					assert.InDelta(t, 100, *body.Expected, 0)
				} else {
					assert.Nil(t, body.ReasonCode)
				}
			}

			require.Len(t, a.repo.recorded, c.wantRows)
			if c.wantRows == 1 {
				row := a.repo.recorded[0]
				assert.Equal(t, credit.AuditReverify, row.Action)
				assert.Equal(t, c.wantStatus, row.Outcome)
				assert.Contains(t, row.RefusalReason, c.wantReason)
				assert.Equal(t, a.adminID.String(), row.ActorID, "the actor is the token's subject")
				assert.Equal(t, "ADMIN", row.ActorRole)
				assert.Equal(t, uid.String(), row.TargetID)
				assert.NotEmpty(t, row.CorrelationID)
			}
		})
	}
}

func TestReverifyStillAnswersWhenTheAuditWriteFails(t *testing.T) {
	a := newAdminRig(t)
	a.repo.result = &credit.ReverifyResult{Outcome: credit.ReverifyStale}
	a.repo.recordEr = errors.New("audit table unavailable")

	rec := a.do(http.MethodPost, adminBase+"/users/"+uuid.NewString()+"/reconcile", a.admin)

	assert.Equal(t, http.StatusOK, rec.Code, "the caller gets the real answer when only the attempt row is lost (D-109)")
}

func TestListAuditLog(t *testing.T) {
	a := newAdminRig(t)
	target := uuid.NewString()

	rec := a.do(http.MethodGet, adminBase+"/audit-log?target_id="+target+"&outcome=refused&from=2026-11-01&limit=10&offset=20", a.admin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var page gen.AuditLogPage
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, "credit.reverify", page.Items[0].Action)
	require.NotNil(t, page.Items[0].BeforeState)
	assert.Equal(t, "FLAGGED", (*page.Items[0].BeforeState)["integrity_status"])

	f := a.repo.filter
	assert.Equal(t, target, f.TargetID)
	assert.Equal(t, credit.OutcomeRefused, f.Outcome)
	assert.Equal(t, 10, f.Limit)
	assert.Equal(t, 20, f.Offset)
	require.NotNil(t, f.From)
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), *f.From, "a bare date is midnight UTC")

	for _, bad := range []string{"from=yesterday", "to=2026-13-40", "outcome=maybe", "offset=-3"} {
		got := a.do(http.MethodGet, adminBase+"/audit-log?"+bad, a.admin)
		assert.Equal(t, http.StatusBadRequest, got.Code, strings.Split(bad, "=")[0])
	}
}
