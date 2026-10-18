//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-15
// Scope: Integration tests of the HTTP surface over the PostgreSQL repository
//   (root AGENTS.md §7): create and its X-Request-Id replay (D-047), the
//   errand's history, accept and a losing accept (D-046), GET /health, and an
//   item with a NUL refused before credits are reserved (D-047).
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/repository"
)

// TestRouterOverPostgres serves the router over the real repository, to catch
// what the in-memory fake accepts but PostgreSQL does not. The subtests share
// one database; each has its own harness and its own students, so none reads
// another's rows.
func TestRouterOverPostgres(t *testing.T) {
	t.Parallel()
	pool := openDatabase(t)
	routerFor := func(h *harness) http.Handler {
		return h.routerWith(ports{repo: repository.NewPostgres(pool), db: pool})
	}

	t.Run("create, replay, read and accept", func(t *testing.T) {
		h := newHarness(t)
		router := routerFor(h)
		requester, courier, late := student(), student(), student()

		first := serve(t, router, http.MethodPost, "/", h.createBody(), as(requester), withRequestID("k-1"))
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
		created := decodeAs[gen.ErrandDetail](t, first)

		again := serve(t, router, http.MethodPost, "/", h.createBody(), as(requester), withRequestID("k-1"))
		require.Equal(t, http.StatusOK, again.Code, again.Body.String())
		assert.Equal(t, created.Errand.Id, decodeAs[gen.ErrandDetail](t, again).Errand.Id)
		assert.Len(t, h.credit.Reserves(), 1, "a replay must not reserve credits again")

		id := "/" + created.Errand.Id.String()
		rec := serve(t, router, http.MethodGet, id, nil, as(requester))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		detail := decodeAs[gen.ErrandDetail](t, rec)
		require.Len(t, detail.History, 1)
		assert.Equal(t, gen.CREATE, detail.History[0].Action)

		accept := map[string]any{"version": created.Errand.Version}
		rec = serve(t, router, http.MethodPost, id+"/accept", accept, as(courier))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		accepted := decodeAs[gen.ErrandDetail](t, rec)
		assert.Equal(t, gen.ErrandStateACCEPTED, accepted.Errand.State)
		require.NotNil(t, accepted.Errand.CourierId)
		assert.Equal(t, courier.UserID, *accepted.Errand.CourierId)

		rec = serve(t, router, http.MethodPost, id+"/accept", accept, as(late))
		requireError(t, rec, http.StatusConflict, "this errand is no longer open")
	})

	t.Run("health", func(t *testing.T) {
		h := newHarness(t)

		rec := serve(t, routerFor(h), http.MethodGet, "/health", nil)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, gen.Health{Status: gen.Ok}, decodeAs[gen.Health](t, rec))
	})

	t.Run("an item with a NUL is refused before credits are reserved", func(t *testing.T) {
		// PostgreSQL text cannot store NUL, so without the check the errand
		// would fail to save only after credit-service had reserved its reward.
		h := newHarness(t)
		body := h.createBody()
		body["itemDescription"] = "latte\u0000"

		rec := serve(t, routerFor(h), http.MethodPost, "/", body, as(student()))

		// AI-generated (edited by James Teo): NUL is one of the control characters the text rule refuses.
		requireError(t, rec, http.StatusBadRequest, "the item description cannot contain line breaks or control characters")
		assert.Empty(t, h.credit.Reserves())
	})
}

// openDatabase starts PostgreSQL, applies the migrations and returns a pool
// over it, closed when the test ends.
func openDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := startPostgres(t)
	require.NoError(t, repository.Migrate(dbURL, "../../migrations"), "apply migrations")
	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// startPostgres starts postgres:16-alpine for the test and returns its URL. It
// copies internal/repository's helper, which lives in a _test file there and so
// cannot be imported.
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "orders",
				"POSTGRES_PASSWORD": "postgres",
				"POSTGRES_USER":     "postgres",
			},
			// The server restarts once after initdb; the second line is the real one.
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err, "start PostgreSQL container")
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return "postgres://postgres:postgres@" + net.JoinHostPort(host, port.Port()) + "/orders?sslmode=disable"
}
