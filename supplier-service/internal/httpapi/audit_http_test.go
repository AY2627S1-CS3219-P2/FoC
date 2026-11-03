// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: HTTP tests for the admin audit log (D-106 to D-115): who an admin write
//   is attributed to, that refused writes are logged and a student's are not,
//   and the admin-only read endpoint and its paging.
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/supplier"
)

func auditRouter(t *testing.T) (http.Handler, *memRepository) {
	t.Helper()
	repo := newMemRepository()
	rev := &fakeRevocations{revokedJTIs: map[string]bool{}}
	return httpapi.NewRouter(supplier.NewService(repo, &zeroErrands{}), startKeySet(t, http.StatusOK), rev), repo
}

func do(router http.Handler, method, path, body, token string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAdminWriteIsAttributedToTheTokenAndTheRequestId(t *testing.T) {
	router, repo := auditRouter(t)
	adminSub := uuid.New()
	token := mintToken(t, tokenSpec{sub: adminSub})

	rec := do(router, http.MethodPost, "/suppliers", supplierBody, token,
		map[string]string{"X-Request-Id": "req-abc", "X-User-Id": uuid.NewString(), "X-User-Role": "STUDENT"})

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, repo.audits, 1)
	e := repo.audits[0]
	assert.Equal(t, adminSub.String(), e.ActorID, "the actor is the token's sub, never a header (D-041)")
	assert.Equal(t, "ADMIN", e.ActorRole)
	assert.Equal(t, "req-abc", e.CorrelationID)
	assert.Equal(t, supplier.ActionCreate, e.Action)
	assert.Equal(t, supplier.OutcomeSucceeded, e.Outcome)
}

func TestRefusedAdminWritesAreLogged(t *testing.T) {
	router, repo := auditRouter(t)
	adminSub := uuid.New()
	token := mintToken(t, tokenSpec{sub: adminSub})

	bad := do(router, http.MethodPost, "/suppliers", `{"name":""}`, token, nil)
	gone := do(router, http.MethodDelete, "/suppliers/"+unknownID, "", token, nil)

	assert.Equal(t, http.StatusBadRequest, bad.Code)
	assert.Equal(t, http.StatusNotFound, gone.Code)
	require.Len(t, repo.audits, 2)
	for _, e := range repo.audits {
		assert.Equal(t, supplier.OutcomeRefused, e.Outcome)
		assert.Equal(t, adminSub.String(), e.ActorID)
	}
	assert.Equal(t, supplier.ActionCreate, repo.audits[0].Action)
	assert.Equal(t, supplier.ActionDelete, repo.audits[1].Action)
	assert.Equal(t, unknownID, repo.audits[1].TargetID)
}

func TestAStudentsRefusedWriteIsNotAnAdminAttempt(t *testing.T) {
	router, repo := auditRouter(t)

	rec := do(router, http.MethodPost, "/suppliers", supplierBody, mintToken(t, tokenSpec{role: "STUDENT"}), nil)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, repo.audits, "only an admin's actions and attempts are logged")
}

func TestAuditLogIsAdminOnly(t *testing.T) {
	router, _ := auditRouter(t)

	cases := []struct {
		name   string
		token  string
		status int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"student", mintToken(t, tokenSpec{role: "STUDENT"}), http.StatusForbidden},
		{"admin", mintToken(t, tokenSpec{}), http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(router, http.MethodGet, "/suppliers/audit-log", "", tc.token, nil)
			assert.Equal(t, tc.status, rec.Code)
		})
	}
}

func TestAnyAdminReadsTheWholeLogIncludingTheirOwnActions(t *testing.T) {
	router, repo := auditRouter(t)
	first := mintToken(t, tokenSpec{sub: uuid.New()})
	second := mintToken(t, tokenSpec{sub: uuid.New()})
	require.Equal(t, http.StatusCreated, do(router, http.MethodPost, "/suppliers", supplierBody, first, nil).Code)

	rec := do(router, http.MethodGet, "/suppliers/audit-log", "", second, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var page struct {
		Items  []map[string]any `json:"items"`
		Total  int              `json:"total"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	assert.Equal(t, 1, page.Total, "a second admin sees the first admin's action")
	assert.Equal(t, 50, page.Limit)
	require.Len(t, page.Items, 1)
	assert.Equal(t, repo.audits[0].ActorID, page.Items[0]["actor_id"])
	for _, field := range []string{"id", "occurred_at", "actor_role", "action", "target_type", "target_id",
		"outcome", "refusal_reason", "reason", "before_state", "after_state", "additional_comments", "correlation_id"} {
		assert.Contains(t, page.Items[0], field)
	}
}

func TestAuditLogRejectsBadParameters(t *testing.T) {
	router, _ := auditRouter(t)
	token := mintToken(t, tokenSpec{})

	for _, q := range []string{"limit=ten", "offset=-1", "offset=x", "from=yesterday", "to=2026-13-45", "outcome=maybe"} {
		t.Run(q, func(t *testing.T) {
			rec := do(router, http.MethodGet, "/suppliers/audit-log?"+q, "", token, nil)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
	for _, q := range []string{"limit=10&offset=20", "from=2026-11-01&to=2026-11-30", "from=2026-11-01T00:00:00Z", "action=supplier.create&outcome=refused&actor_id=x&target_id=y"} {
		t.Run(q, func(t *testing.T) {
			rec := do(router, http.MethodGet, "/suppliers/audit-log?"+q, "", token, nil)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestAuditLogDoesNotShadowASupplierId(t *testing.T) {
	router, _ := auditRouter(t)

	rec := do(router, http.MethodGet, "/suppliers/"+storedID, "", "", nil)

	assert.Equal(t, http.StatusOK, rec.Code, "the public read of one supplier still works without a token")
}
