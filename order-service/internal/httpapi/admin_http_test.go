// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: HTTP tests for the admin dashboard on order-service (D-104 to D-117):
//   the past-errands list and its filters, the audit log, a user's errand
//   counts, and that an admin's cancel over HTTP is logged with the token's
//   identity and the request id.
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

func TestPastErrandsAreListedForAnAdminOnlyAndPagedWithATotal(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier, adm := student(), student(), admin()
	oldest := h.put(order.StateCompleted, requester, &courier)
	h.clock.Advance(time.Hour)
	middle := h.put(order.StateCancelled, requester, nil)
	h.clock.Advance(time.Hour)
	newest := h.put(order.StateExpired, requester, nil)
	h.put(order.StateOpen, requester, nil) // not past

	t.Run("an admin gets final errands, newest first, with the total", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/?scope=history", nil, as(adm))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		list := decodeAs[gen.ErrandList](t, rec)
		require.Len(t, list.Items, 3)
		assert.Equal(t, []uuid.UUID{newest.ID, middle.ID, oldest.ID}, []uuid.UUID{list.Items[0].Id, list.Items[1].Id, list.Items[2].Id})
		require.NotNil(t, list.Total)
		assert.Equal(t, 3, *list.Total)
		assert.Equal(t, 50, *list.Limit)
		assert.Equal(t, 0, *list.Offset)
	})
	t.Run("filters and paging reach the service", func(t *testing.T) {
		q := url.Values{"state": {"CANCELLED"}, "sort": {"supplier_name"}, "limit": {"1"}, "offset": {"0"},
			"supplier_id": {h.supplier.ID.String()}, "user_id": {requester.UserID.String()},
			"from": {"2020-01-01"}, "to": {"2100-01-01T00:00:00Z"}}
		rec := h.do(t, http.MethodGet, "/?scope=history&"+q.Encode(), nil, as(adm))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		list := decodeAs[gen.ErrandList](t, rec)
		require.Len(t, list.Items, 1)
		assert.Equal(t, middle.ID, list.Items[0].Id)
		assert.Equal(t, 1, *list.Total)
	})
	t.Run("a student is refused", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/?scope=history", nil, as(requester))
		requireError(t, rec, http.StatusForbidden, "only an admin can list every errand")
	})
	t.Run("input that cannot mean anything is a 400", func(t *testing.T) {
		for _, target := range []string{
			"/?scope=history&state=OPEN", "/?scope=history&from=yesterday", "/?scope=history&to=2026-13-45",
			"/?scope=history&offset=-1", "/?scope=history&sort=random", "/?scope=history&limit=ten",
			"/?scope=history&supplier_id=not-a-uuid", "/?scope=active&state=COMPLETED",
		} {
			rec := h.do(t, http.MethodGet, target, nil, as(adm))
			assert.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", target, rec.Body.String())
		}
	})
	t.Run("the original active list is unchanged when no option is given", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/?scope=active", nil, as(adm))
		require.Equal(t, http.StatusOK, rec.Code)
		list := decodeAs[gen.ErrandList](t, rec)
		assert.Nil(t, list.Total, "no paging fields on the original list")
	})
}

func TestAnAdminsCancelOverHTTPIsLoggedWithTheTokensIdentityAndTheRequestId(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, adm := student(), admin()
	o := h.put(order.StateOpen, requester, nil)

	rec := h.do(t, http.MethodPost, "/"+o.ID.String()+"/cancel", nil, as(adm), withRequestID("req-abc"),
		withHeader("X-User-Id", uuid.NewString()), withHeader("X-User-Role", "STUDENT"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rows := h.repo.Audits()
	require.Len(t, rows, 1)
	assert.Equal(t, adm.UserID.String(), rows[0].ActorID, "the actor is the token's sub, never a header (D-041)")
	assert.Equal(t, order.AuditCancel, rows[0].Action)
	assert.Equal(t, "req-abc", rows[0].CorrelationID)

	// A refused attempt over HTTP is logged too (D-109).
	rec = h.do(t, http.MethodPost, "/"+o.ID.String()+"/complete", nil, as(adm), withRequestID("req-def"))
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	rows = h.repo.Audits()
	require.Len(t, rows, 2)
	assert.Equal(t, order.OutcomeRefused, rows[1].Outcome)
	assert.Equal(t, order.AuditComplete, rows[1].Action)
}

func TestAnyAdminReadsTheWholeAuditLog(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, first, second := student(), admin(), admin()
	o := h.put(order.StateOpen, requester, nil)
	require.Equal(t, http.StatusOK, h.do(t, http.MethodPost, "/"+o.ID.String()+"/cancel", nil, as(first)).Code)

	t.Run("a second admin sees the first admin's action", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/audit-log?action=errand.cancel&outcome=succeeded&limit=10&offset=0", nil, as(second))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		page := decodeAs[gen.AuditLogPage](t, rec)
		require.Equal(t, 1, page.Total)
		require.Len(t, page.Items, 1)
		e := page.Items[0]
		assert.Equal(t, first.UserID, uuid.UUID(e.ActorId))
		assert.Equal(t, o.ID.String(), e.TargetId)
		assert.Equal(t, gen.AuditEntryOutcome("succeeded"), e.Outcome)
		require.NotNil(t, e.BeforeState)
		assert.Equal(t, "OPEN", (*e.BeforeState)["state"])
		assert.Equal(t, "CANCELLED", (*e.AfterState)["state"])
	})
	t.Run("a student is refused", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/audit-log", nil, as(requester))
		requireError(t, rec, http.StatusForbidden, "only an admin can read the audit log")
	})
	t.Run("bad input is a 400", func(t *testing.T) {
		for _, target := range []string{"/audit-log?limit=ten", "/audit-log?offset=-1", "/audit-log?from=yesterday", "/audit-log?outcome=maybe", "/audit-log?to=2026-13-45"} {
			rec := h.do(t, http.MethodGet, target, nil, as(second))
			assert.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", target, rec.Body.String())
		}
	})
	t.Run("a bare errand id is still an errand, not the log", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/"+o.ID.String(), nil, as(second))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestErrandCountsForTheAccountReview(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	user, courier, adm := student(), student(), admin()
	h.put(order.StateCompleted, user, &courier)
	h.put(order.StateCancelled, user, nil)
	h.put(order.StateCancelled, user, nil)
	h.put(order.StateCompleted, courier, &user)

	rec := h.do(t, http.MethodGet, "/users/"+user.UserID.String()+"/errand-counts", nil, as(adm))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	counts := decodeAs[gen.ErrandCounts](t, rec)
	assert.Equal(t, map[string]int{"COMPLETED": 1, "CANCELLED": 2}, counts.Requested)
	assert.Equal(t, map[string]int{"COMPLETED": 1}, counts.Assigned)
	requireError(t, h.do(t, http.MethodGet, "/users/"+user.UserID.String()+"/errand-counts", nil, as(user)),
		http.StatusForbidden, "only an admin can count another user's errands")
	assert.Equal(t, http.StatusBadRequest, h.do(t, http.MethodGet, "/users/not-a-uuid/errand-counts", nil, as(adm)).Code)
}
