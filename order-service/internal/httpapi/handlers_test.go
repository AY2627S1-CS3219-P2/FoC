// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Tests every route of api/openapi.yaml through the full router:
//   create and replay (D-047), validation, error statuses (D-046), visibility (D-045)
//   and allowed actions (D-053), history, lists and the supplier count (D-052).
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestCreateAnswers201ThenReplaysTheSameRequestIDWith200(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester := student()

	first := h.do(t, http.MethodPost, "/", h.createBody(), as(requester), withRequestID("create-once"))

	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	created := decodeAs[gen.ErrandDetail](t, first)
	assert.Equal(t, gen.ErrandStateOPEN, created.Errand.State)
	assert.Equal(t, 1, created.Errand.Version)
	assert.Equal(t, 5, created.Errand.Reward)
	assert.Equal(t, "COM3 Lobby", created.Errand.DeliveryLocation)
	assert.Equal(t, gen.SupplierSnapshot{Id: h.supplier.ID, Name: "Spinelli Coffee", Location: supplierLocation}, created.Errand.Supplier)
	assert.Equal(t, []gen.ErrandAction{gen.Amend, gen.Cancel}, created.Errand.Actions)
	require.NotNil(t, created.Errand.ExpiresAt)
	assert.WithinDuration(t, h.clock.Now().Add(expiryPeriod), *created.Errand.ExpiresAt, 0)
	assert.WithinDuration(t, h.clock.Now(), created.ServerTime, 0)
	require.Len(t, created.History, 1)
	assert.Equal(t, gen.CREATE, created.History[0].Action)

	again := h.do(t, http.MethodPost, "/", h.createBody(), as(requester), withRequestID("create-once"))

	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	replayed := decodeAs[gen.ErrandDetail](t, again)
	assert.Equal(t, created.Errand.Id, replayed.Errand.Id)
	assert.Len(t, h.credit.Reserves(), 1, "a replay must not reserve credits again")
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester := student()
	errand := "/" + h.put(order.StateOpen, requester, nil).ID.String()
	createWith := func(field string, value any) map[string]any {
		b := h.createBody()
		b[field] = value
		return b
	}

	cases := []struct {
		name   string
		method string
		target string
		body   any
		opts   []reqOption
		msg    string // the exact message, or
		substr string // part of one worded by encoding/json
	}{
		{name: "unknown JSON field", method: http.MethodPost, target: "/", body: createWith("colour", "red"), substr: `unknown field "colour"`},
		{
			name: "X-Request-Id sent twice on create", method: http.MethodPost, target: "/", body: h.createBody(),
			opts: []reqOption{withHeader("X-Request-Id", "one"), withHeader("X-Request-Id", "two")}, msg: "send X-Request-Id only once",
		},
		{name: "errand id not a UUID", method: http.MethodGet, target: "/not-a-uuid", msg: "errandId must be a UUID"},
		{name: "action on an id that is not a UUID", method: http.MethodPost, target: "/123/accept", body: `{"version":1}`, msg: "errandId must be a UUID"},
		{name: "supplier id in the path not a UUID", method: http.MethodGet, target: "/suppliers/spinelli/active-errands", msg: "supplierId must be a UUID"},
		{name: "delivery location not on the list", method: http.MethodPost, target: "/", body: createWith("deliveryLocation", "Mars"), msg: "choose a delivery location from the campus list"},
		{name: "item of 281 characters", method: http.MethodPost, target: "/", body: createWith("itemDescription", strings.Repeat("x", 281)), msg: "the item description can be at most 280 characters"},
		{name: "note of 141 characters", method: http.MethodPost, target: "/", body: createWith("deliveryNote", strings.Repeat("x", 141)), msg: "the note can be at most 140 characters"},
		// AI-generated (edited by James Teo): the text rule create and amend share.
		{name: "item of only a zero-width space", method: http.MethodPost, target: "/", body: createWith("itemDescription", "\u200b"), msg: "describe the item to pick up"},
		{name: "item with a control character", method: http.MethodPost, target: "/", body: createWith("itemDescription", "gnp.exe\u0007"), msg: "the item description cannot contain line breaks or control characters"},
		{name: "item with a right-to-left override", method: http.MethodPost, target: "/", body: createWith("itemDescription", "\u202egnp.exe"), msg: "the item description cannot contain text-direction control characters"},
		{name: "note with a tab", method: http.MethodPost, target: "/", body: createWith("deliveryNote", "Hall 3\tRoom 12"), msg: "the note can contain newlines but no other line breaks or control characters"},
		// AI-generated (edited by James Teo).
		{name: "note with a line separator", method: http.MethodPost, target: "/", body: createWith("deliveryNote", "Hall 3\u2028Room 12"), msg: "the note can contain newlines but no other line breaks or control characters"},
		{name: "item with a paragraph separator", method: http.MethodPost, target: "/", body: createWith("itemDescription", "Two kaya\u2029toast sets"), msg: "the item description cannot contain line breaks or control characters"},
		{name: "item of only Hangul fillers", method: http.MethodPost, target: "/", body: createWith("itemDescription", "\u3164\u3164"), msg: "describe the item to pick up"},
		{name: "amended note with a direction isolate", method: http.MethodPatch, target: errand, body: map[string]any{"version": 3, "deliveryNote": "Room \u206621\u2069"}, msg: "the note cannot contain text-direction control characters"},
		{name: "reward of zero", method: http.MethodPost, target: "/", body: createWith("reward", 0), msg: "the reward must be at least 1 credit"},
		{name: "reward not a number", method: http.MethodPost, target: "/", body: createWith("reward", "five"), msg: "reward has the wrong type"},
		{name: "supplier id in the body not a UUID", method: http.MethodPost, target: "/", body: createWith("supplierId", "spinelli"), substr: "invalid value"},
		{name: "no body", method: http.MethodPost, target: "/", msg: "a JSON request body is required"},
		{name: "malformed JSON", method: http.MethodPost, target: "/", body: `{"reward":`, msg: "the request body is not valid JSON"},
		{name: "body not an object", method: http.MethodPost, target: "/", body: `[]`, msg: "the request body must be a JSON object"},
		{name: "two JSON values", method: http.MethodPost, target: errand + "/accept", body: `{"version":3}{"version":4}`, msg: "the request body must be a single JSON object"},
		{name: "accept without a version", method: http.MethodPost, target: errand + "/accept", body: `{}`, msg: "version is required"},
		{name: "amend without a version", method: http.MethodPatch, target: errand, body: `{"reward":6}`, msg: "version is required"},
		{name: "amend changing nothing", method: http.MethodPatch, target: errand, body: `{"version":3}`, msg: "send at least one field to change"},
		{name: "missing scope", method: http.MethodGet, target: "/requested", msg: "scope is required"},
		{name: "scope not in the contract", method: http.MethodGet, target: "/assigned?scope=everything", msg: "scope must be active or history"},
		{name: "requested scope not in the contract", method: http.MethodGet, target: "/requested?scope=everything", msg: "scope must be active or history"},
		{name: "scope sent twice", method: http.MethodGet, target: "/requested?scope=active&scope=history", msg: "scope is not valid"},
		{name: "admin list without a scope", method: http.MethodGet, target: "/", msg: "scope is required"},
		{name: "admin list with the history scope", method: http.MethodGet, target: "/?scope=history", msg: "scope must be active"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, tc.method, tc.target, tc.body, append([]reqOption{as(requester)}, tc.opts...)...)

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			got := decodeAs[gen.Error](t, rec).Error
			if tc.substr != "" {
				assert.Contains(t, got, tc.substr)
			} else {
				assert.Equal(t, tc.msg, got)
			}
		})
	}
	assert.Empty(t, h.credit.Reserves(), "no invalid create may reserve credits")
}

func TestServiceErrorsMapToStatusCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		run    func(t *testing.T, h *harness) *httptest.ResponseRecorder
		status int
		msg    string
	}{
		{
			name: "accepting your own errand",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				requester := student()
				d := h.create(t, requester)
				return h.do(t, http.MethodPost, "/"+d.Errand.Id.String()+"/accept", map[string]any{"version": d.Errand.Version}, as(requester))
			},
			status: http.StatusForbidden, msg: "you cannot accept your own errand",
		},
		{
			name: "a student listing every errand",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				return h.do(t, http.MethodGet, "/?scope=active", nil, as(student()))
			},
			status: http.StatusForbidden, msg: "only an admin can list every errand",
		},
		{
			name: "an admin posting an errand",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				return h.do(t, http.MethodPost, "/", h.createBody(), as(admin()))
			},
			status: http.StatusForbidden, msg: "only students can post errands",
		},
		{
			name: "an errand that does not exist",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				return h.do(t, http.MethodGet, "/"+uuid.NewString(), nil, as(student()))
			},
			status: http.StatusNotFound, msg: "errand not found",
		},
		{
			name: "accepting with a stale version",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				requester := student()
				d := h.create(t, requester)
				id := "/" + d.Errand.Id.String()
				rec := h.do(t, http.MethodPatch, id, map[string]any{"version": 1, "reward": 6}, as(requester))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				return h.do(t, http.MethodPost, id+"/accept", map[string]any{"version": 1}, as(student()))
			},
			status: http.StatusConflict, msg: "this errand has changed since you loaded it; refresh and try again",
		},
		{
			name: "accepting an errand another courier holds",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				courier := student()
				o := h.put(order.StateAccepted, student(), &courier)
				return h.do(t, http.MethodPost, "/"+o.ID.String()+"/accept", map[string]any{"version": o.Version}, as(student()))
			},
			status: http.StatusConflict, msg: "this errand is no longer open",
		},
		// AI-generated (edited by James Teo).
		{
			name: "a sixth open errand",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				requester := student()
				for range order.MaxOpenErrands {
					h.create(t, requester)
				}
				return h.do(t, http.MethodPost, "/", h.createBody(), as(requester))
			},
			status: http.StatusConflict, msg: "you already have 5 open errands; cancel one or wait for one to be accepted before posting another",
		},
		{
			name: "accepting an errand you withdrew from",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				courier := student()
				d := h.create(t, student())
				id := "/" + d.Errand.Id.String()
				rec := h.do(t, http.MethodPost, id+"/accept", map[string]any{"version": d.Errand.Version}, as(courier))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				rec = h.do(t, http.MethodPost, id+"/withdraw", nil, as(courier))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				reopened := decodeAs[gen.ErrandDetail](t, rec).Errand
				return h.do(t, http.MethodPost, id+"/accept", map[string]any{"version": reopened.Version}, as(courier))
			},
			status: http.StatusConflict, msg: "you withdrew from this errand, so you cannot accept it again",
		},
		{
			name: "credit-service refusing the reservation",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				h.credit.ReserveHook = func(order.Reservation) (bool, error) { return false, order.ErrInsufficientCredits }
				return h.do(t, http.MethodPost, "/", h.createBody(), as(student()))
			},
			status: http.StatusUnprocessableEntity, msg: "not enough available credits for this reward",
		},
		{
			name: "supplier-service down",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				h.suppliers.FailWith(ordertest.ErrInjected)
				return h.do(t, http.MethodPost, "/", h.createBody(), as(student()))
			},
			status: http.StatusServiceUnavailable, msg: "supplier-service is not answering; please try again",
		},
		{
			name: "an unexpected failure",
			run: func(t *testing.T, h *harness) *httptest.ResponseRecorder {
				caller := student()
				o := h.put(order.StateOpen, caller, nil)
				h.repo.FailNext("Get", ordertest.ErrInjected)
				return h.do(t, http.MethodGet, "/"+o.ID.String(), nil, as(caller))
			},
			status: http.StatusInternalServerError, msg: "internal error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			requireError(t, tc.run(t, h), tc.status, tc.msg)
		})
	}
}

func TestUnexpectedErrorIsLoggedWithTheRequestIDNotSent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	caller := student()
	o := h.put(order.StateOpen, caller, nil)
	h.repo.FailNext("Get", ordertest.ErrInjected)

	rec := h.do(t, http.MethodGet, "/"+o.ID.String(), nil, as(caller), withRequestID("boom-1"))

	requireError(t, rec, http.StatusInternalServerError, "internal error")
	assert.NotContains(t, rec.Body.String(), "injected", "the cause stays in the log")
	line := h.findLog(t, func(l map[string]any) bool { return l["level"] == "ERROR" })
	assert.Equal(t, "boom-1", line["request_id"])
	assert.Contains(t, line["err"], "injected failure")
}

func TestACommittedActionIsNotReportedAsFailedWhenTheReReadFails(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	o := h.put(order.StateAccepted, requester, &courier)
	router := h.routerWith(ports{repo: historyFails{h.repo}})

	rec := serve(t, router, http.MethodPost, "/"+o.ID.String()+"/pickup", nil, as(courier), withRequestID("reread-1"))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := decodeAs[gen.ErrandDetail](t, rec)
	assert.Equal(t, gen.ErrandStatePICKEDUP, d.Errand.State)
	assert.Equal(t, []gen.ErrandAction{gen.Deliver}, d.Errand.Actions)
	assert.Empty(t, d.History)
	stored, err := h.repo.Get(t.Context(), o.ID)
	require.NoError(t, err)
	assert.Equal(t, order.StatePickedUp, stored.State)
	line := h.findLog(t, func(l map[string]any) bool { return l["level"] == "WARN" && l["request_id"] == "reread-1" })
	assert.Contains(t, line["err"], "injected failure")
}

func TestVisibility(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier, stranger, adm := student(), student(), student(), admin()
	open := h.put(order.StateOpen, requester, nil)
	accepted := h.put(order.StateAccepted, requester, &courier)

	t.Run("party fields", func(t *testing.T) {
		cases := []struct {
			name    string
			viewer  order.Caller
			errand  order.Order
			private bool
		}{
			{name: "stranger on an OPEN errand", viewer: stranger, errand: open},
			{name: "requester on an OPEN errand", viewer: requester, errand: open, private: true},
			{name: "admin on an OPEN errand", viewer: adm, errand: open, private: true},
			{name: "courier on an ACCEPTED errand", viewer: courier, errand: accepted, private: true},
			{name: "requester on an ACCEPTED errand", viewer: requester, errand: accepted, private: true},
			{name: "admin on an ACCEPTED errand", viewer: adm, errand: accepted, private: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rec := h.do(t, http.MethodGet, "/"+tc.errand.ID.String(), nil, as(tc.viewer))

				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				errand := errandJSON(t, rec)
				if !tc.private {
					assert.NotContains(t, errand, "requesterId")
					assert.NotContains(t, errand, "deliveryNote")
					assert.NotContains(t, rec.Body.String(), requester.UserID.String())
					return
				}
				assert.Equal(t, requester.UserID.String(), errand["requesterId"])
				assert.Equal(t, "by the vending machine", errand["deliveryNote"])
			})
		}
	})

	t.Run("a stranger cannot see an errand that is not open", func(t *testing.T) {
		pastDeadline := h.newOrder(order.StateOpen, requester, nil)
		passed := h.clock.Now().Add(-time.Second)
		pastDeadline.ExpiresAt = &passed
		h.repo.Put(pastDeadline)
		hidden := []order.Order{
			accepted,
			h.put(order.StatePickedUp, requester, &courier),
			h.put(order.StateDelivered, requester, &courier),
			h.put(order.StateCompleted, requester, &courier),
			h.put(order.StateCancelled, requester, nil),
			h.put(order.StateExpired, requester, nil),
			pastDeadline,
		}
		for _, o := range hidden {
			rec := h.do(t, http.MethodGet, "/"+o.ID.String(), nil, as(stranger))
			requireError(t, rec, http.StatusNotFound, "errand not found")
			if o.ID == pastDeadline.ID {
				continue // still OPEN, so its accept is refused as expired instead
			}
			// Nor may an accept name the state the GET hides (D-045).
			rec = h.do(t, http.MethodPost, "/"+o.ID.String()+"/accept", map[string]any{"version": o.Version}, as(stranger))
			requireError(t, rec, http.StatusConflict, "this errand is no longer open")
		}
	})

	t.Run("lists show each viewer their own view", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/open", nil, as(stranger))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		list := decodeAs[gen.ErrandList](t, rec)
		require.Len(t, list.Items, 1)
		assert.Equal(t, open.ID, list.Items[0].Id)
		assert.Nil(t, list.Items[0].RequesterId)
		assert.Nil(t, list.Items[0].DeliveryNote)
		assert.Equal(t, []gen.ErrandAction{gen.Accept}, list.Items[0].Actions)

		rec = h.do(t, http.MethodGet, "/open", nil, as(requester))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		list = decodeAs[gen.ErrandList](t, rec)
		require.Len(t, list.Items, 1)
		require.NotNil(t, list.Items[0].RequesterId)
		assert.Equal(t, requester.UserID, *list.Items[0].RequesterId)
		assert.Equal(t, []gen.ErrandAction{gen.Amend, gen.Cancel}, list.Items[0].Actions)
	})
}

func TestOptionalFieldsShowOnlyInTheirState(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier, adm := student(), student(), admin()
	cases := []struct {
		state   order.State
		courier *order.Caller
		present []string
		absent  []string
	}{
		{state: order.StateOpen, present: []string{"expiresAt"}, absent: []string{"confirmBy", "courierId"}},
		{state: order.StateAccepted, courier: &courier, present: []string{"courierId"}, absent: []string{"expiresAt", "confirmBy"}},
		{state: order.StatePickedUp, courier: &courier, present: []string{"courierId"}, absent: []string{"expiresAt", "confirmBy"}},
		{state: order.StateDelivered, courier: &courier, present: []string{"courierId", "confirmBy"}, absent: []string{"expiresAt"}},
		{state: order.StateCompleted, courier: &courier, present: []string{"courierId"}, absent: []string{"expiresAt", "confirmBy"}},
		{state: order.StateExpired, absent: []string{"expiresAt", "confirmBy", "courierId"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			o := h.newOrder(tc.state, requester, tc.courier)
			// Both deadlines set whatever the state: the response must still
			// show each only in the state it runs in.
			expires, confirmBy := h.clock.Now().Add(time.Hour), h.clock.Now().Add(2*time.Hour)
			o.ExpiresAt, o.ConfirmBy = &expires, &confirmBy
			h.repo.Put(o)

			rec := h.do(t, http.MethodGet, "/"+o.ID.String(), nil, as(adm))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			errand := errandJSON(t, rec)
			for _, key := range tc.present {
				assert.Contains(t, errand, key)
			}
			for _, key := range tc.absent {
				assert.NotContains(t, errand, key)
			}
		})
	}
}

func TestActionsListWhatTheViewerMayDoNow(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier, stranger, adm := student(), student(), student(), admin()
	none := []gen.ErrandAction{}

	cases := []struct {
		name     string
		state    order.State
		viewer   order.Caller
		expired  bool
		want     []gen.ErrandAction
		assigned bool
	}{
		{name: "requester, OPEN", state: order.StateOpen, viewer: requester, want: []gen.ErrandAction{gen.Amend, gen.Cancel}},
		{name: "stranger, OPEN", state: order.StateOpen, viewer: stranger, want: []gen.ErrandAction{gen.Accept}},
		{name: "admin, OPEN", state: order.StateOpen, viewer: adm, want: []gen.ErrandAction{gen.Cancel}},
		{name: "requester, OPEN past its deadline", state: order.StateOpen, viewer: requester, expired: true, want: none},
		{name: "requester, ACCEPTED", state: order.StateAccepted, viewer: requester, assigned: true, want: none},
		{name: "courier, ACCEPTED", state: order.StateAccepted, viewer: courier, assigned: true, want: []gen.ErrandAction{gen.Withdraw, gen.Pickup}},
		{name: "admin, ACCEPTED", state: order.StateAccepted, viewer: adm, assigned: true, want: []gen.ErrandAction{gen.Cancel}},
		{name: "requester, PICKED_UP", state: order.StatePickedUp, viewer: requester, assigned: true, want: none},
		{name: "courier, PICKED_UP", state: order.StatePickedUp, viewer: courier, assigned: true, want: []gen.ErrandAction{gen.Deliver}},
		{name: "admin, PICKED_UP", state: order.StatePickedUp, viewer: adm, assigned: true, want: []gen.ErrandAction{gen.Cancel, gen.Complete}},
		{name: "requester, DELIVERED", state: order.StateDelivered, viewer: requester, assigned: true, want: []gen.ErrandAction{gen.Confirm}},
		{name: "courier, DELIVERED", state: order.StateDelivered, viewer: courier, assigned: true, want: none},
		{name: "admin, DELIVERED", state: order.StateDelivered, viewer: adm, assigned: true, want: []gen.ErrandAction{gen.Cancel, gen.Complete}},
		{name: "requester, COMPLETED", state: order.StateCompleted, viewer: requester, assigned: true, want: none},
		{name: "courier, COMPLETED", state: order.StateCompleted, viewer: courier, assigned: true, want: none},
		{name: "admin, COMPLETED", state: order.StateCompleted, viewer: adm, assigned: true, want: none},
		{name: "requester, CANCELLED", state: order.StateCancelled, viewer: requester, want: none},
		{name: "admin, EXPIRED", state: order.StateExpired, viewer: adm, want: none},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var held *order.Caller
			if tc.assigned {
				held = &courier
			}
			o := h.newOrder(tc.state, requester, held)
			if tc.expired {
				passed := h.clock.Now().Add(-time.Second)
				o.ExpiresAt = &passed
			}
			h.repo.Put(o)

			rec := h.do(t, http.MethodGet, "/"+o.ID.String(), nil, as(tc.viewer))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, tc.want, decodeAs[gen.ErrandDetail](t, rec).Errand.Actions)
			_, isArray := errandJSON(t, rec)["actions"].([]any)
			assert.True(t, isArray, "actions is an array, never null")
			// The fixture has no history rows: still an array.
			assert.Equal(t, []any{}, decodeAs[map[string]any](t, rec)["history"])
		})
	}
}

func TestEachActionRouteRunsItsAction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		from    order.State
		actor   string
		route   string
		body    any
		to      gen.ErrandState
		history gen.HistoryAction
		// event is whether the action queues a settlement event (D-048).
		event bool
		check func(t *testing.T, h *harness, d gen.ErrandDetail)
	}{
		{name: "accept", from: order.StateOpen, actor: "stranger", route: "accept", body: map[string]any{"version": 3}, to: gen.ErrandStateACCEPTED, history: gen.ACCEPT},
		{
			name: "withdraw", from: order.StateAccepted, actor: "courier", route: "withdraw", to: gen.ErrandStateOPEN, history: gen.WITHDRAW,
			check: func(t *testing.T, h *harness, d gen.ErrandDetail) {
				assert.Nil(t, d.Errand.CourierId)
				require.NotNil(t, d.Errand.ExpiresAt)
				assert.WithinDuration(t, h.clock.Now().Add(expiryPeriod), *d.Errand.ExpiresAt, 0)
			},
		},
		{name: "pickup", from: order.StateAccepted, actor: "courier", route: "pickup", to: gen.ErrandStatePICKEDUP, history: gen.PICKUP},
		{
			name: "deliver", from: order.StatePickedUp, actor: "courier", route: "deliver", to: gen.ErrandStateDELIVERED, history: gen.DELIVER,
			check: func(t *testing.T, h *harness, d gen.ErrandDetail) {
				require.NotNil(t, d.Errand.ConfirmBy)
				assert.WithinDuration(t, h.clock.Now().Add(confirmWindow), *d.Errand.ConfirmBy, 0)
			},
		},
		{name: "confirm", from: order.StateDelivered, actor: "requester", route: "confirm", to: gen.ErrandStateCOMPLETED, history: gen.CONFIRM, event: true},
		{name: "cancel by the requester", from: order.StateOpen, actor: "requester", route: "cancel", to: gen.ErrandStateCANCELLED, history: gen.CANCEL, event: true},
		{name: "cancel by an admin", from: order.StateAccepted, actor: "admin", route: "cancel", to: gen.ErrandStateCANCELLED, history: gen.ADMINCANCEL, event: true},
		{name: "complete by an admin", from: order.StatePickedUp, actor: "admin", route: "complete", to: gen.ErrandStateCOMPLETED, history: gen.ADMINCOMPLETE, event: true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			requester, courier := student(), student()
			actors := map[string]order.Caller{"requester": requester, "courier": courier, "stranger": student(), "admin": admin()}
			var held *order.Caller
			if tc.from != order.StateOpen {
				held = &courier
			}
			o := h.put(tc.from, requester, held)
			requestID := fmt.Sprintf("req-%s-%d", tc.route, i)

			rec := h.do(t, http.MethodPost, "/"+o.ID.String()+"/"+tc.route, tc.body, as(actors[tc.actor]), withRequestID(requestID))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			d := decodeAs[gen.ErrandDetail](t, rec)
			assert.Equal(t, tc.to, d.Errand.State)
			require.Len(t, d.History, 1, "the fixture starts with no history")
			assert.Equal(t, tc.history, d.History[0].Action)
			require.NotNil(t, d.History[0].FromState)
			assert.Equal(t, gen.ErrandState(tc.from), *d.History[0].FromState)
			assert.Equal(t, tc.to, d.History[0].ToState)
			// The request's id is the correlation id of everything the action
			// wrote (D-047, NFR7.1).
			history, err := h.repo.History(t.Context(), o.ID)
			require.NoError(t, err)
			require.Len(t, history, 1)
			assert.Equal(t, requestID, history[0].CorrelationID)
			if tc.event {
				events := h.repo.EventsFor(o.ID)
				require.Len(t, events, 1)
				assert.Equal(t, requestID, events[0].CorrelationID)
				assert.Equal(t, requestID, events[0].Payload.CorrelationID)
			}
			if tc.check != nil {
				tc.check(t, h, d)
			}
		})
	}

	t.Run("amend", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		requester := student()
		o := h.put(order.StateOpen, requester, nil)

		rec := h.do(t, http.MethodPatch, "/"+o.ID.String(), map[string]any{"version": 3, "reward": 6}, as(requester), withRequestID("req-amend"))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		// The credit call and the history row carry the request's id (D-047, NFR7.1).
		adjusts := h.credit.Adjusts()
		require.Len(t, adjusts, 1)
		assert.Equal(t, "req-amend", adjusts[0].CorrelationID)
		history, err := h.repo.History(t.Context(), o.ID)
		require.NoError(t, err)
		require.Len(t, history, 1)
		assert.Equal(t, order.ActionAmend, history[0].Action)
		assert.Equal(t, "req-amend", history[0].CorrelationID)
	})
}

func TestLifecycleThroughTheAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	id := "/" + h.create(t, requester).Errand.Id.String()
	step := func(caller order.Caller, method, target string, body any, want gen.ErrandState) gen.ErrandDetail {
		t.Helper()
		h.clock.Advance(time.Minute)
		rec := h.do(t, method, target, body, as(caller))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		d := decodeAs[gen.ErrandDetail](t, rec)
		require.Equal(t, want, d.Errand.State)
		return d
	}

	d := step(requester, http.MethodPatch, id, map[string]any{"version": 1, "reward": 7, "deliveryNote": "call me"}, gen.ErrandStateOPEN)
	assert.Equal(t, 2, d.Errand.Version)
	assert.Equal(t, 7, d.Errand.Reward)
	require.NotNil(t, d.Errand.DeliveryNote)
	assert.Equal(t, "call me", *d.Errand.DeliveryNote)

	d = step(courier, http.MethodPost, id+"/accept", map[string]any{"version": 2}, gen.ErrandStateACCEPTED)
	require.NotNil(t, d.Errand.CourierId)
	assert.Equal(t, courier.UserID, *d.Errand.CourierId)
	assert.Equal(t, []gen.ErrandAction{gen.Withdraw, gen.Pickup}, d.Errand.Actions)

	step(courier, http.MethodPost, id+"/pickup", nil, gen.ErrandStatePICKEDUP)
	step(courier, http.MethodPost, id+"/deliver", nil, gen.ErrandStateDELIVERED)
	d = step(requester, http.MethodPost, id+"/confirm", nil, gen.ErrandStateCOMPLETED)
	assert.Empty(t, d.Errand.Actions)

	var actions []gen.HistoryAction
	for _, e := range d.History {
		actions = append(actions, e.Action)
	}
	assert.Equal(t, []gen.HistoryAction{gen.CREATE, gen.AMEND, gen.ACCEPT, gen.PICKUP, gen.DELIVER, gen.CONFIRM}, actions)
	assert.Nil(t, d.History[0].FromState, "CREATE has no from-state")
	for i, e := range d.History[1:] {
		assert.NotNil(t, e.FromState, "entry %d", i+1)
		if e.Action != gen.AMEND {
			assert.Nil(t, e.Changes, "only AMEND carries changes, not %s", e.Action)
		}
	}
	require.NotNil(t, d.History[1].Changes)
	assert.Equal(t, []gen.FieldChange{
		{Field: gen.DeliveryNote, From: "by the vending machine", To: "call me"},
		{Field: gen.Reward, From: "5", To: "7"},
	}, *d.History[1].Changes)
	assert.Len(t, h.credit.Reserves(), 1)
	adjusts := h.credit.Adjusts()
	require.Len(t, adjusts, 1)
	assert.Equal(t, 7, adjusts[0].NewReward)

	// The history names no person: who acted is in the action itself.
	rec := h.do(t, http.MethodGet, id, nil, as(requester))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	history, ok := decodeAs[map[string]any](t, rec)["history"].([]any)
	require.True(t, ok)
	allowed := []string{"action", "fromState", "toState", "occurredAt", "changes"}
	for _, entry := range history {
		fields, ok := entry.(map[string]any)
		require.True(t, ok)
		for key := range fields {
			assert.Contains(t, allowed, key)
		}
	}
}

func TestHistoryHidesDeliveryNoteChangesFromStrangers(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, stranger := student(), student()
	id := "/" + h.create(t, requester).Errand.Id.String()
	amend := func(body map[string]any) {
		t.Helper()
		rec := h.do(t, http.MethodPatch, id, body, as(requester))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	amend(map[string]any{"version": 1, "deliveryNote": "room 12, knock twice", "reward": 6})
	amend(map[string]any{"version": 2, "deliveryNote": "room 14"})

	rec := h.do(t, http.MethodGet, id, nil, as(stranger))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "room 1")
	seen := decodeAs[gen.ErrandDetail](t, rec)
	require.Len(t, seen.History, 3)
	require.NotNil(t, seen.History[1].Changes)
	assert.Equal(t, []gen.FieldChange{{Field: gen.Reward, From: "5", To: "6"}}, *seen.History[1].Changes)
	require.NotNil(t, seen.History[2].Changes, "every AMEND entry carries changes")
	assert.Empty(t, *seen.History[2].Changes, "a note-only amendment shows no changes to a stranger")

	rec = h.do(t, http.MethodGet, id, nil, as(requester))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	own := decodeAs[gen.ErrandDetail](t, rec)
	require.NotNil(t, own.History[2].Changes)
	assert.Equal(t, []gen.FieldChange{{Field: gen.DeliveryNote, From: "room 12, knock twice", To: "room 14"}}, *own.History[2].Changes)
}

// AI-generated (edited by James Teo).
func TestHistoryHidesDeliveryNoteValuesFromTheCourier(t *testing.T) {
	// The courier reads the note as it is now. An AMEND entry shows them that
	// the note changed, and when, but not a note the requester has since
	// cleared or replaced; the requester still sees both values.
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	id := "/" + h.create(t, requester).Errand.Id.String()
	rec := h.do(t, http.MethodPatch, id, map[string]any{"version": 1, "deliveryNote": "hp 9123 4567, hall 3 room 12"}, as(requester))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(t, http.MethodPatch, id, map[string]any{"version": 2, "deliveryNote": ""}, as(requester))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(t, http.MethodPost, id+"/accept", map[string]any{"version": 3}, as(courier))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = h.do(t, http.MethodGet, id, nil, as(courier))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "9123")
	seen := decodeAs[gen.ErrandDetail](t, rec)
	require.Len(t, seen.History, 4)
	for _, entry := range seen.History[1:3] {
		require.NotNil(t, entry.Changes)
		assert.Equal(t, []gen.FieldChange{{Field: gen.DeliveryNote, From: "", To: ""}}, *entry.Changes)
	}

	rec = h.do(t, http.MethodGet, id, nil, as(requester))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	own := decodeAs[gen.ErrandDetail](t, rec)
	require.NotNil(t, own.History[2].Changes)
	assert.Equal(t, []gen.FieldChange{{Field: gen.DeliveryNote, From: "hp 9123 4567, hall 3 room 12", To: ""}}, *own.History[2].Changes)
}

// AI-generated (edited by James Teo).
func TestAcceptIsNotOfferedToACourierWhoWithdrew(t *testing.T) {
	// D-060: the open list and the errand itself both leave accept out of the
	// actions of the courier who gave it back, and keep it for everyone else.
	t.Parallel()
	h := newHarness(t)
	courier, other := student(), student()
	d := h.create(t, student())
	id := "/" + d.Errand.Id.String()
	rec := h.do(t, http.MethodPost, id+"/accept", map[string]any{"version": d.Errand.Version}, as(courier))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(t, http.MethodPost, id+"/withdraw", nil, as(courier))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, decodeAs[gen.ErrandDetail](t, rec).Errand.Actions, "the withdraw's own answer")

	for _, viewer := range []struct {
		name string
		as   order.Caller
		want []gen.ErrandAction
	}{
		{"the courier who withdrew", courier, []gen.ErrandAction{}},
		{"another student", other, []gen.ErrandAction{gen.Accept}},
	} {
		rec = h.do(t, http.MethodGet, id, nil, as(viewer.as))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, viewer.want, decodeAs[gen.ErrandDetail](t, rec).Errand.Actions, viewer.name)

		rec = h.do(t, http.MethodGet, "/open", nil, as(viewer.as))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		list := decodeAs[gen.ErrandList](t, rec)
		require.Len(t, list.Items, 1, viewer.name)
		assert.Equal(t, viewer.want, list.Items[0].Actions, viewer.name)
	}
}

func TestAssignedLists(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	act := func(caller order.Caller, id uuid.UUID, route string, body any) {
		t.Helper()
		rec := h.do(t, http.MethodPost, "/"+id.String()+"/"+route, body, as(caller))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	released := h.create(t, requester, withRequestID("errand-a")).Errand.Id
	completed := h.create(t, requester, withRequestID("errand-b")).Errand.Id

	act(courier, released, "accept", map[string]any{"version": 1})
	h.clock.Advance(time.Minute)
	act(courier, released, "withdraw", nil)
	releasedAt := h.clock.Now()
	h.clock.Advance(time.Minute)
	act(courier, completed, "accept", map[string]any{"version": 1})
	act(courier, completed, "pickup", nil)
	act(courier, completed, "deliver", nil)
	h.clock.Advance(time.Minute)
	act(requester, completed, "confirm", nil)
	completedAt := h.clock.Now()

	t.Run("history", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/assigned?scope=history", nil, as(courier))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, []any{}, decodeAs[map[string]any](t, rec)["items"])
		list := decodeAs[gen.AssignedList](t, rec)
		require.Len(t, list.History, 2, "most recent first")
		want := []struct {
			id      uuid.UUID
			outcome gen.AssignmentRecordOutcome
			endedAt time.Time
		}{
			{id: completed, outcome: gen.AssignmentRecordOutcomeCOMPLETED, endedAt: completedAt},
			{id: released, outcome: gen.AssignmentRecordOutcomeRELEASED, endedAt: releasedAt},
		}
		for i, w := range want {
			got := list.History[i]
			assert.Equal(t, w.id, got.ErrandId)
			assert.Equal(t, w.outcome, got.Outcome)
			assert.WithinDuration(t, w.endedAt, got.EndedAt, 0)
			assert.Equal(t, gen.SupplierSnapshot{Id: h.supplier.ID, Name: "Spinelli Coffee", Location: supplierLocation}, got.Supplier)
			assert.Equal(t, "1 x iced latte, less ice", got.ItemDescription)
			assert.Equal(t, "COM3 Lobby", got.DeliveryLocation)
			assert.Equal(t, 5, got.Reward)
		}
		assert.WithinDuration(t, h.clock.Now(), list.ServerTime, 0)
	})

	t.Run("active", func(t *testing.T) {
		current := h.create(t, requester, withRequestID("errand-c")).Errand.Id
		act(courier, current, "accept", map[string]any{"version": 1})

		rec := h.do(t, http.MethodGet, "/assigned?scope=active", nil, as(courier))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, []any{}, decodeAs[map[string]any](t, rec)["history"])
		list := decodeAs[gen.AssignedList](t, rec)
		require.Len(t, list.Items, 1)
		assert.Equal(t, current, list.Items[0].Id)
		assert.Equal(t, []gen.ErrandAction{gen.Withdraw, gen.Pickup}, list.Items[0].Actions)
	})
}

func TestEmptyListsAreArraysNotNull(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	cases := []struct {
		target string
		viewer order.Caller
		arrays []string
	}{
		{target: "/open", viewer: student(), arrays: []string{"items"}},
		{target: "/requested?scope=active", viewer: student(), arrays: []string{"items"}},
		{target: "/requested?scope=history", viewer: student(), arrays: []string{"items"}},
		{target: "/assigned?scope=active", viewer: student(), arrays: []string{"items", "history"}},
		{target: "/assigned?scope=history", viewer: student(), arrays: []string{"items", "history"}},
		{target: "/?scope=active", viewer: admin(), arrays: []string{"items"}},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, tc.target, nil, as(tc.viewer))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			body := decodeAs[map[string]any](t, rec)
			for _, key := range tc.arrays {
				assert.Equal(t, []any{}, body[key], "%s must be an empty array, not null", key)
			}
			assert.Contains(t, body, "serverTime")
		})
	}
}

func TestAdminListsEveryErrandThatIsNotFinal(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier, adm := student(), student(), admin()
	stalled := h.put(order.StateAccepted, requester, &courier)
	h.clock.Advance(time.Minute)
	recent := h.put(order.StateOpen, requester, nil)
	h.put(order.StateCompleted, requester, &courier)
	h.put(order.StateCancelled, requester, nil)

	rec := h.do(t, http.MethodGet, "/?scope=active", nil, as(adm))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := decodeAs[gen.ErrandList](t, rec)
	require.Len(t, list.Items, 2)
	assert.Equal(t, stalled.ID, list.Items[0].Id, "oldest state change first")
	assert.Equal(t, recent.ID, list.Items[1].Id)
	for _, e := range list.Items {
		assert.Equal(t, []gen.ErrandAction{gen.Cancel}, e.Actions)
	}
	assert.WithinDuration(t, h.clock.Now(), list.ServerTime, 0)
}

func TestRequestedLists(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	active := h.put(order.StateDelivered, requester, &courier)
	done := h.put(order.StateCompleted, requester, &courier)
	h.put(order.StateOpen, student(), nil) // someone else's

	cases := []struct {
		scope string
		want  uuid.UUID
	}{
		{scope: "active", want: active.ID},
		{scope: "history", want: done.ID},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, "/requested?scope="+tc.scope, nil, as(requester))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			list := decodeAs[gen.ErrandList](t, rec)
			require.Len(t, list.Items, 1)
			assert.Equal(t, tc.want, list.Items[0].Id)
		})
	}
}

func TestLocations(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	rec := h.do(t, http.MethodGet, "/locations", nil, as(student()))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := decodeAs[gen.LocationList](t, rec)
	assert.Equal(t, order.DeliveryLocations(), list.Items)
	assert.Contains(t, list.Items, "The Deck, AS8")
}

func TestCountSupplierActiveErrands(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester, courier := student(), student()
	h.put(order.StateOpen, requester, nil)
	h.put(order.StateAccepted, requester, &courier)
	h.put(order.StateCompleted, requester, &courier)
	elsewhere := h.newOrder(order.StateOpen, requester, nil)
	elsewhere.Supplier.ID = uuid.New()
	h.repo.Put(elsewhere)

	cases := []struct {
		name     string
		supplier uuid.UUID
		want     int
	}{
		{name: "supplier with errands", supplier: h.supplier.ID, want: 2},
		{name: "supplier with none", supplier: uuid.New(), want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, "/suppliers/"+tc.supplier.String()+"/active-errands", nil, as(admin()))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, gen.SupplierActiveErrands{SupplierId: tc.supplier, ActiveErrands: tc.want},
				decodeAs[gen.SupplierActiveErrands](t, rec))
		})
	}

	t.Run("a student is refused", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/suppliers/"+h.supplier.ID.String()+"/active-errands", nil, as(requester))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})
	t.Run("no token is a 401", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/suppliers/"+h.supplier.ID.String()+"/active-errands", nil)
		requireError(t, rec, http.StatusUnauthorized, msgNoIdentity)
	})
}

// TestDomainNamesMatchTheContract guards the plain string conversions in
// dto.go: every name the domain can produce must be one the contract lists.
func TestDomainNamesMatchTheContract(t *testing.T) {
	t.Parallel()
	for _, s := range slices.Concat(order.ActiveStates(), order.FinalStates()) {
		assert.True(t, gen.ErrandState(s).Valid(), "state %s", s)
	}
	for _, a := range []order.Action{
		order.ActionCreate, order.ActionAmend, order.ActionAccept, order.ActionWithdraw,
		order.ActionPickUp, order.ActionDeliver, order.ActionConfirm, order.ActionAutoConfirm,
		order.ActionCancel, order.ActionExpire, order.ActionAdminCancel, order.ActionAdminComplete,
	} {
		assert.True(t, gen.HistoryAction(a).Valid(), "history action %s", a)
	}
	for _, a := range []order.APIAction{
		order.APIAmend, order.APIAccept, order.APIWithdraw, order.APIPickUp,
		order.APIDeliver, order.APIConfirm, order.APICancel, order.APIComplete,
	} {
		assert.True(t, gen.ErrandAction(a).Valid(), "action %s", a)
	}
	for _, f := range []string{
		order.FieldSupplier, order.FieldItemDescription, order.FieldDeliveryLocation,
		order.FieldDeliveryNote, order.FieldReward,
	} {
		assert.True(t, gen.FieldChangeField(f).Valid(), "field %s", f)
	}
	for _, o := range []order.AssignmentOutcome{order.OutcomeCompleted, order.OutcomeCancelled, order.OutcomeReleased} {
		assert.True(t, gen.AssignmentRecordOutcome(o).Valid(), "outcome %s", o)
	}
}
