// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: HTTP tests for the rating routes (D-093) and Find errands' records
//   (D-092).
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

// completedErrand takes a new errand from post to COMPLETED.
func completedErrand(t *testing.T, h *harness, requester, courier order.Caller) uuid.UUID {
	t.Helper()
	id := h.create(t, requester).Errand.Id
	for _, step := range []struct {
		caller order.Caller
		route  string
		body   any
	}{
		{courier, "accept", map[string]any{"version": 1}},
		{courier, "pickup", nil},
		{courier, "deliver", nil},
		{requester, "confirm", nil},
	} {
		rec := h.do(t, http.MethodPost, "/"+id.String()+"/"+step.route, step.body, as(step.caller))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	return id
}

func TestRatingAnErrand(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	id := completedErrand(t, h, requester, courier)

	rec := h.do(t, http.MethodPost, "/"+id.String()+"/rating", map[string]any{"stars": 4}, as(requester))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeAs[gen.Rating](t, rec)
	assert.Equal(t, gen.Rating{
		ErrandId: id, RaterId: requester.UserID, RateeId: courier.UserID,
		RaterRole: gen.RaterRole("requester"), Stars: 4, RatedAt: h.clock.Now(),
	}, got)

	again := h.do(t, http.MethodPost, "/"+id.String()+"/rating", map[string]any{"stars": 1}, as(requester))
	requireError(t, again, http.StatusConflict, "you have already rated this errand")

	missing := h.do(t, http.MethodPost, "/"+id.String()+"/rating", map[string]any{}, as(courier))
	requireError(t, missing, http.StatusBadRequest, "stars is required")

	tooMany := h.do(t, http.MethodPost, "/"+id.String()+"/rating", map[string]any{"stars": 6}, as(courier))
	requireError(t, tooMany, http.StatusBadRequest, "stars must be a whole number from 1 to 5")
}

func TestPendingRatingsAndTheGate(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	id := completedErrand(t, h, requester, courier)

	rec := h.do(t, http.MethodGet, "/ratings/pending", nil, as(courier))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := decodeAs[gen.PendingRatingList](t, rec)
	require.Len(t, list.Items, 1)
	assert.Equal(t, id, list.Items[0].ErrandId)
	assert.Equal(t, requester.UserID, list.Items[0].RateeId)
	assert.Equal(t, gen.RaterRole("courier"), list.Items[0].RaterRole)

	blocked := h.do(t, http.MethodPost, "/", map[string]any{
		"supplierId": h.supplier.ID, "itemDescription": "x", "deliveryLocation": "COM3 Lobby", "reward": 1,
	}, as(requester))
	requireError(t, blocked, http.StatusConflict, "rate your completed errand before posting or accepting another")

	rate := h.do(t, http.MethodPost, "/"+id.String()+"/rating", map[string]any{"stars": 5}, as(courier))
	require.Equal(t, http.StatusCreated, rate.Code, rate.Body.String())
	rec = h.do(t, http.MethodGet, "/ratings/pending", nil, as(courier))
	assert.Empty(t, decodeAs[gen.PendingRatingList](t, rec).Items)
	assert.Equal(t, []any{}, decodeAs[map[string]any](t, rec)["items"], "an empty list, not null")
}

func TestUserRatingIsPublic(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier, onlooker := student(), student(), student()
	id := completedErrand(t, h, requester, courier)
	rec := h.do(t, http.MethodPost, "/"+id.String()+"/rating", map[string]any{"stars": 3}, as(requester))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = h.do(t, http.MethodGet, "/users/"+courier.UserID.String()+"/rating", nil, as(onlooker))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := decodeAs[gen.UserRating](t, rec)
	assert.Equal(t, 1, got.Count)
	require.NotNil(t, got.Average)
	assert.InDelta(t, 3, *got.Average, 1e-9)

	rec = h.do(t, http.MethodGet, "/users/"+onlooker.UserID.String()+"/rating", nil, as(onlooker))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, decodeAs[map[string]any](t, rec), "average", "no average for a user nobody rated")
}

func TestRecordingFindErrandsActivity(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	courier := student()
	open := h.create(t, student()).Errand.Id
	filters := map[string]any{"timeLeft": "under1h", "from": "", "to": "area:UTown"}

	rec := h.do(t, http.MethodPost, "/activity/shown", map[string]any{
		"items":   []any{map[string]any{"errandId": open, "position": 0}},
		"filters": filters,
	}, as(courier))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rows := h.repo.ShownRows()
	require.Len(t, rows, 1)
	assert.Equal(t, order.Shown{OrderID: open, Position: 0}, rows[0].Shown)
	assert.Equal(t, order.Filters{TimeLeft: "under1h", To: "area:UTown"}, rows[0].Filters)

	rec = h.do(t, http.MethodPost, "/activity/searches", map[string]any{"filters": filters}, as(courier))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Len(t, h.repo.SearchRows(), 1)

	noFilters := h.do(t, http.MethodPost, "/activity/searches", map[string]any{}, as(courier))
	requireError(t, noFilters, http.StatusBadRequest, "filters with timeLeft, from and to is required")

	noPosition := h.do(t, http.MethodPost, "/activity/shown", map[string]any{
		"items":   []any{map[string]any{"errandId": open}},
		"filters": filters,
	}, as(courier))
	requireError(t, noPosition, http.StatusBadRequest, "each item needs errandId and position")

	badTimeLeft := h.do(t, http.MethodPost, "/activity/searches", map[string]any{
		"filters": map[string]any{"timeLeft": "soon", "from": "", "to": ""},
	}, as(courier))
	require.Equal(t, http.StatusBadRequest, badTimeLeft.Code, badTimeLeft.Body.String())

	byAdmin := h.do(t, http.MethodPost, "/activity/searches", map[string]any{"filters": filters}, as(admin()))
	require.Equal(t, http.StatusForbidden, byAdmin.Code, byAdmin.Body.String())
}

func TestTheNewRoutesNeedAToken(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, r := range []struct{ method, target string }{
		{http.MethodPost, "/" + uuid.NewString() + "/rating"},
		{http.MethodGet, "/ratings/pending"},
		{http.MethodGet, "/users/" + uuid.NewString() + "/rating"},
		{http.MethodPost, "/activity/shown"},
		{http.MethodPost, "/activity/searches"},
	} {
		rec := h.do(t, r.method, r.target, map[string]any{})
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", r.method, r.target)
	}
}
