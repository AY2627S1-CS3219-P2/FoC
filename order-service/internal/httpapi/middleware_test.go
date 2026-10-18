// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Tests the middleware chain through the full router: token
//   authentication (D-054, D-056), request id and access log (D-047, NFR7.1),
//   panic recovery, body limit, write deadline, health, and the JSON 404 and 405.
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestAuthentication(t *testing.T) {
	// D-056: order-service verifies the bearer token itself; any X-User-Id or
	// X-User-Role is ignored. Only GET /health needs no token.
	t.Parallel()
	h := newHarness(t)
	id := uuid.NewString()

	cases := []struct {
		name   string
		method string
		target string
		opts   []reqOption
		want   int
	}{
		{name: "no token", method: http.MethodGet, target: "/open", want: http.StatusUnauthorized},
		{name: "the old gateway headers alone", method: http.MethodGet, target: "/open",
			opts: []reqOption{withHeader("X-User-Id", id), withHeader("X-User-Role", "ADMIN")}, want: http.StatusUnauthorized},
		{name: "not a bearer token", method: http.MethodGet, target: "/open",
			opts: []reqOption{withHeader("Authorization", "Basic "+tokenFor(id, "STUDENT"))}, want: http.StatusUnauthorized},
		{name: "bearer with no token", method: http.MethodGet, target: "/open",
			opts: []reqOption{withHeader("Authorization", "Bearer ")}, want: http.StatusUnauthorized},
		{name: "two Authorization headers", method: http.MethodGet, target: "/open",
			opts: []reqOption{withHeader("Authorization", "Bearer "+tokenFor(id, "STUDENT")), withHeader("Authorization", "Bearer "+tokenFor(id, "STUDENT"))}, want: http.StatusUnauthorized},
		{name: "a token that does not verify", method: http.MethodGet, target: "/open", opts: []reqOption{withToken("forged")}, want: http.StatusUnauthorized},
		{name: "role in lower case", method: http.MethodGet, target: "/open", opts: []reqOption{withToken(tokenFor(id, "student"))}, want: http.StatusUnauthorized},
		{name: "unknown role", method: http.MethodGet, target: "/open", opts: []reqOption{withToken(tokenFor(id, "COURIER"))}, want: http.StatusUnauthorized},
		{name: "write route", method: http.MethodPost, target: "/" + uuid.NewString() + "/cancel", want: http.StatusUnauthorized},
		{name: "unknown route still needs a token", method: http.MethodGet, target: "/" + uuid.NewString() + "/bogus", want: http.StatusUnauthorized},
		{name: "POST /health is not the public route", method: http.MethodPost, target: "/health", want: http.StatusUnauthorized},
		{name: "supplier count needs a token", method: http.MethodGet, target: "/suppliers/" + uuid.NewString() + "/active-errands", want: http.StatusUnauthorized},
		{name: "student", method: http.MethodGet, target: "/open", opts: []reqOption{withToken(tokenFor(id, "STUDENT"))}, want: http.StatusOK},
		{name: "admin", method: http.MethodGet, target: "/open", opts: []reqOption{withToken(tokenFor(id, "ADMIN"))}, want: http.StatusOK},
		{name: "scheme in lower case", method: http.MethodGet, target: "/open",
			opts: []reqOption{withHeader("Authorization", "bearer "+tokenFor(id, "STUDENT"))}, want: http.StatusOK},
		{name: "health needs none", method: http.MethodGet, target: "/health", want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, tc.method, tc.target, nil, tc.opts...)
			if tc.want == http.StatusUnauthorized {
				requireError(t, rec, http.StatusUnauthorized, msgNoIdentity)
				return
			}
			require.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestTheTokenNotTheHeadersNamesTheCaller(t *testing.T) {
	// A client sending another user's X-User-Id alongside its own token acts
	// as itself (D-056).
	t.Parallel()
	h := newHarness(t)
	caller, other := student(), student()

	h.do(t, http.MethodGet, "/open", nil, as(caller), withHeader("X-User-Id", other.UserID.String()),
		withHeader("X-User-Role", "ADMIN"), withRequestID("spoofed-headers"))

	assert.Equal(t, caller.UserID.String(), h.accessLine(t, "spoofed-headers")["user_id"])
	rec := h.do(t, http.MethodGet, "/?scope=active", nil, as(caller), withHeader("X-User-Role", "ADMIN"))
	assert.Equal(t, http.StatusForbidden, rec.Code, "the role comes from the token: %s", rec.Body.String())
}

func TestRevokedTokensAreRefused(t *testing.T) {
	// D-054: a logged-out or suspended token is a 401, like an expired one.
	t.Parallel()
	h := newHarness(t)
	token := tokenFor(uuid.NewString(), "STUDENT")
	require.Equal(t, http.StatusOK, h.do(t, http.MethodGet, "/open", nil, withToken(token)).Code)

	h.revoked.revoke(strings.Split(token, "|")[3])

	requireError(t, h.do(t, http.MethodGet, "/open", nil, withToken(token)), http.StatusUnauthorized, msgNoIdentity)
}

func TestRedisDownSkipsTheRevocationCheck(t *testing.T) {
	// D-056: when Redis does not answer, the request goes on with a verified
	// token and the skipped check is logged at ERROR.
	t.Parallel()
	h := newHarness(t)
	caller := student()
	h.revoked.setDown(true)

	rec := h.do(t, http.MethodGet, "/open", nil, as(caller), withRequestID("redis-down"))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	line := h.findLog(t, func(l map[string]any) bool {
		return l["level"] == "ERROR" && l["request_id"] == "redis-down"
	})
	assert.Contains(t, line["msg"], "revocation check skipped")
	assert.Equal(t, caller.UserID.String(), line["user_id"])
}

func TestUnavailableKeysAreA503(t *testing.T) {
	// D-056: without user-service's keys nothing can be verified, so the
	// request is refused, and not with a 401 the frontend would answer by
	// refreshing the token.
	t.Parallel()
	h := newHarness(t)
	h.tokens.keysDown.Store(true)

	rec := h.do(t, http.MethodGet, "/open", nil, as(student()), withRequestID("keys-down"))

	requireError(t, rec, http.StatusServiceUnavailable, "sign-in cannot be checked right now; please try again")
	line := h.findLog(t, func(l map[string]any) bool {
		return l["level"] == "ERROR" && l["request_id"] == "keys-down"
	})
	assert.Contains(t, line["msg"], "signing keys")
	assert.Equal(t, http.StatusOK, h.do(t, http.MethodGet, "/health", nil).Code, "health needs no keys")
}

func TestTheTokenReachesCreditCalls(t *testing.T) {
	// D-056: a create forwards the requester's own token to credit-service.
	t.Parallel()
	h := newHarness(t)
	credit := &tokenCredit{Credit: h.credit}
	router := h.routerWith(ports{credit: credit})
	caller := student()
	token := tokenFor(caller.UserID.String(), "STUDENT")

	rec := serve(t, router, http.MethodPost, "/", h.createBody(), withToken(token), withRequestID("create-with-token"))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{token}, credit.seen())
}

// tokenCredit records the token on the context of each Reserve.
type tokenCredit struct {
	*ordertest.Credit

	mu     sync.Mutex
	tokens []string
}

func (c *tokenCredit) Reserve(ctx context.Context, r order.Reservation) error {
	token, _ := auth.TokenFromContext(ctx)
	c.mu.Lock()
	c.tokens = append(c.tokens, token)
	c.mu.Unlock()
	return c.Credit.Reserve(ctx, r)
}

func (c *tokenCredit) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.tokens)
}

func TestHealth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pingErr error
		status  int
		want    gen.HealthStatus
	}{
		{name: "database answers", status: http.StatusOK, want: gen.Ok},
		{name: "database down", pingErr: errors.New("connection refused"), status: http.StatusServiceUnavailable, want: gen.Unhealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.db.setErr(tc.pingErr)

			rec := h.do(t, http.MethodGet, "/health", nil)

			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Equal(t, gen.Health{Status: tc.want}, decodeAs[gen.Health](t, rec))
			left := h.db.lastTimeLeft()
			assert.Greater(t, left, time.Duration(0), "the ping must run under a deadline")
			assert.LessOrEqual(t, left, 2*time.Second)
		})
	}
}

func TestRequestIDIsEchoedOnlyWhenWellFormed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	cases := []struct {
		name string
		sent []string
		keep bool
	}{
		{name: "well formed", sent: []string{"req-123_ABC.4:5/6"}, keep: true},
		{name: "64 characters", sent: []string{strings.Repeat("a", 64)}, keep: true},
		{name: "contains a space", sent: []string{"has space"}, keep: true},
		{name: "not ASCII", sent: []string{"réquest"}, keep: true},
		{name: "none sent"},
		{name: "empty", sent: []string{""}},
		{name: "65 characters", sent: []string{strings.Repeat("a", 65)}},
		{name: "not valid UTF-8", sent: []string{"r\xe9quest"}},
		// 64 characters, but 256 bytes: one more than an AMQP CorrelationId holds.
		{name: "64 four-byte characters", sent: []string{strings.Repeat("😀", 64)}},
		{name: "sent twice", sent: []string{"first", "second"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opts []reqOption
			for _, v := range tc.sent {
				opts = append(opts, withHeader("X-Request-Id", v))
			}

			rec := h.do(t, http.MethodGet, "/health", nil, opts...)

			got := rec.Header().Get("X-Request-Id")
			if tc.keep {
				assert.Equal(t, tc.sent[0], got)
			} else {
				_, err := uuid.Parse(got)
				assert.NoError(t, err, "a replaced id is a fresh UUID, got %q", got)
			}
			// The id the response carries is the one the log carries.
			assert.Equal(t, float64(http.StatusOK), h.accessLine(t, got)["status"])
		})
	}
}

func TestRequestIDReachesTheService(t *testing.T) {
	t.Parallel()

	t.Run("a well-formed id is the correlation id and the duplicate key", func(t *testing.T) {
		h := newHarness(t)
		requester := student()

		d := h.create(t, requester, withRequestID("create-7f3a"))

		reserves := h.credit.Reserves()
		require.Len(t, reserves, 1)
		assert.Equal(t, "create-7f3a", reserves[0].CorrelationID)
		stored, err := h.repo.FindByRequestKey(t.Context(), requester.UserID, "create-7f3a")
		require.NoError(t, err)
		assert.Equal(t, d.Errand.Id, stored.ID)
	})

	t.Run("an id with spaces is both the correlation id and the duplicate key", func(t *testing.T) {
		h := newHarness(t)
		requester := student()

		rec := h.do(t, http.MethodPost, "/", h.createBody(), as(requester), withRequestID("key with spaces"))

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, "key with spaces", rec.Header().Get("X-Request-Id"))
		reserves := h.credit.Reserves()
		require.Len(t, reserves, 1)
		assert.Equal(t, "key with spaces", reserves[0].CorrelationID)
		stored, err := h.repo.Get(t.Context(), decodeAs[gen.ErrandDetail](t, rec).Errand.Id)
		require.NoError(t, err)
		assert.Equal(t, "key with spaces", stored.RequestKey)
	})

	// A key the correlation id could not keep is refused rather than stored,
	// so a stored key is always the id its request's logs and events carry.
	refused := []struct {
		name string
		key  string
	}{
		{name: "a key that is not valid UTF-8 is refused", key: "caf\xe9"},
		{name: "a key of 256 bytes is refused", key: strings.Repeat("😀", 64)},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			rec := h.do(t, http.MethodPost, "/", h.createBody(), as(student()), withRequestID(tc.key))

			requireError(t, rec, http.StatusBadRequest, "X-Request-Id must be valid UTF-8 text of at most 255 bytes")
			assert.Empty(t, h.credit.Reserves())
		})
	}

	t.Run("no id means no duplicate key", func(t *testing.T) {
		h := newHarness(t)

		d := h.create(t, student())

		stored, err := h.repo.Get(t.Context(), d.Errand.Id)
		require.NoError(t, err)
		assert.Empty(t, stored.RequestKey)
	})

	t.Run("an id longer than the contract allows is refused", func(t *testing.T) {
		h := newHarness(t)

		rec := h.do(t, http.MethodPost, "/", h.createBody(), as(student()), withRequestID(strings.Repeat("k", 65)))

		requireError(t, rec, http.StatusBadRequest, "X-Request-Id can be at most 64 characters")
		assert.Empty(t, h.credit.Reserves())
	})
}

func TestAccessLog(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	caller := student()

	t.Run("an identified request logs its user", func(t *testing.T) {
		h.do(t, http.MethodGet, "/open", nil, as(caller), withRequestID("log-identified"))

		line := h.accessLine(t, "log-identified")
		assert.Equal(t, "INFO", line["level"])
		assert.Equal(t, caller.UserID.String(), line["user_id"])
		assert.Equal(t, http.MethodGet, line["method"])
		assert.Equal(t, "/open", line["path"])
		assert.Equal(t, float64(http.StatusOK), line["status"])
		assert.IsType(t, float64(0), line["duration_ms"])
	})

	t.Run("a public route has no user", func(t *testing.T) {
		h.do(t, http.MethodGet, "/health", nil, withRequestID("log-public"))

		line := h.accessLine(t, "log-public")
		assert.NotContains(t, line, "user_id")
	})

	t.Run("a refused identity is logged without a user", func(t *testing.T) {
		h.do(t, http.MethodGet, "/open", nil, withRequestID("log-refused"))

		line := h.accessLine(t, "log-refused")
		assert.Equal(t, float64(http.StatusUnauthorized), line["status"])
		assert.NotContains(t, line, "user_id")
	})
}

func TestPanicBecomesA500(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	router := h.routerWith(ports{db: panicPinger{}})

	rec := serve(t, router, http.MethodGet, "/health", nil, withRequestID("panic-1"))

	requireError(t, rec, http.StatusInternalServerError, "internal error")
	assert.Equal(t, "panic-1", rec.Header().Get("X-Request-Id"))
	line := h.findLog(t, func(l map[string]any) bool { return l["level"] == "ERROR" })
	assert.Equal(t, "panic-1", line["request_id"])
	assert.Contains(t, line["panic"], "database driver exploded")
	assert.Equal(t, float64(http.StatusInternalServerError), h.accessLine(t, "panic-1")["status"])
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requester := student()
	open := h.put(order.StateOpen, requester, nil)
	tooBig := strings.Repeat("a", 17<<10)

	cases := []struct {
		name   string
		method string
		target string
		body   any
		status int
		msg    string
	}{
		{
			name: "POST with a declared length over 16 KiB", method: http.MethodPost, target: "/",
			body: `{"itemDescription":"` + tooBig + `"}`, status: http.StatusRequestEntityTooLarge, msg: "request body too large",
		},
		{
			name: "POST streamed past 16 KiB", method: http.MethodPost, target: "/",
			body:   io.MultiReader(strings.NewReader(`{"itemDescription":"` + tooBig + `"}`)),
			status: http.StatusRequestEntityTooLarge, msg: "request body too large",
		},
		{
			name: "PATCH over 16 KiB", method: http.MethodPatch, target: "/" + open.ID.String(),
			body: `{"version":3,"itemDescription":"` + tooBig + `"}`, status: http.StatusRequestEntityTooLarge, msg: "request body too large",
		},
		{
			// Exactly at the limit the body is read, and fails on its content.
			name: "POST of exactly 16 KiB", method: http.MethodPost, target: "/",
			body:   bodyOfSize(16<<10, h.supplier.ID),
			status: http.StatusBadRequest, msg: "the item description can be at most 280 characters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, tc.method, tc.target, tc.body, as(requester))
			requireError(t, rec, tc.status, tc.msg)
		})
	}
	t.Run("POST to a body-less action over 16 KiB ignores the body", func(t *testing.T) {
		// The action never reads its body, and its contract lists no 413.
		own := h.put(order.StateOpen, requester, nil)

		rec := h.do(t, http.MethodPost, "/"+own.ID.String()+"/cancel", tooBig, as(requester))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, gen.ErrandStateCANCELLED, decodeAs[gen.ErrandDetail](t, rec).Errand.State)
	})
	assert.Empty(t, h.credit.Reserves())
}

// bodyOfSize is a create body of exactly n bytes whose item description is
// far too long.
func bodyOfSize(n int, supplierID uuid.UUID) string {
	prefix := `{"supplierId":"` + supplierID.String() + `","deliveryLocation":"COM3 Lobby","reward":5,"itemDescription":"`
	suffix := `"}`
	return prefix + strings.Repeat("a", n-len(prefix)-len(suffix)) + suffix
}

func TestWritesRunUnderATenSecondDeadline(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	dir := &deadlineDirectory{Suppliers: h.suppliers}
	router := h.routerWith(ports{dir: dir})
	requester := student()

	rec := serve(t, router, http.MethodPost, "/", h.createBody(), as(requester))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeAs[gen.ErrandDetail](t, rec)
	rec = serve(t, router, http.MethodPatch, "/"+created.Errand.Id.String(), map[string]any{"version": 1, "reward": 6}, as(requester))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	recorded := dir.recorded()
	require.Len(t, recorded, 2, "create and amend each look the supplier up once")
	for _, left := range recorded {
		assert.Greater(t, left, 9*time.Second)
		assert.LessOrEqual(t, left, 10*time.Second)
	}
}

func TestUnknownRoutesAndMethodsAnswerJSON(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := uuid.NewString()
	cases := []struct {
		name   string
		method string
		target string
		status int
		msg    string
		allow  string
	}{
		{name: "unknown path", method: http.MethodGet, target: "/" + id + "/bogus", status: http.StatusNotFound, msg: "no such route"},
		{name: "unknown action", method: http.MethodPost, target: "/" + id + "/teleport", status: http.StatusNotFound, msg: "no such route"},
		{name: "DELETE an errand", method: http.MethodDelete, target: "/" + id, status: http.StatusMethodNotAllowed, msg: "method not allowed", allow: "GET, PATCH"},
		{name: "PUT the collection", method: http.MethodPut, target: "/", status: http.StatusMethodNotAllowed, msg: "method not allowed", allow: "GET, POST"},
		{name: "GET an action", method: http.MethodGet, target: "/" + id + "/accept", status: http.StatusMethodNotAllowed, msg: "method not allowed", allow: "POST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, tc.method, tc.target, nil, as(student()))
			requireError(t, rec, tc.status, tc.msg)
			assert.Equal(t, tc.allow, rec.Header().Get("Allow"))
		})
	}
}
