//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Integration tests of the migrations, the repository, and the service
//   and HTTP surface on a real PostgreSQL (root AGENTS.md §7).
// Author review: PENDING — <reviewer to complete>

package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"foc/notification-service/internal/auth"
	"foc/notification-service/internal/gen"
	"foc/notification-service/internal/httpapi"
	"foc/notification-service/internal/notification"
	"foc/notification-service/internal/repository"
)

type env struct {
	pool *pgxpool.Pool
	repo *repository.Postgres
	svc  *notification.Service
}

// TestPostgres runs every test against one PostgreSQL container. Each works on
// fresh random ids, so none depends on another's rows or on the order they run in.
func TestPostgres(t *testing.T) {
	e := newEnv(t)
	t.Run("a redelivered event stores nothing new", e.testRedelivery)
	t.Run("an event's rows are all or nothing", e.testAtomic)
	t.Run("a late older event does not change the courier", e.testOutOfOrderCourier)
	t.Run("a cancel reaches the courier on the errand", e.testCancelReachesCourier)
	t.Run("the list is the caller's own, newest first, in pages", e.testList)
	t.Run("mark read only touches the caller's own", e.testMarkRead)
	t.Run("the HTTP surface", e.testHTTP)
	t.Run("the down migration undoes the up migration", e.testDownMigration)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "notifications",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	url := fmt.Sprintf("postgres://postgres:postgres@%s/notifications?sslmode=disable", net.JoinHostPort(host, port.Port()))

	require.NoError(t, repository.Migrate(url, filepath.Join("..", "..", "migrations")))
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := repository.NewPostgres(pool)
	return &env{pool: pool, repo: repo, svc: notification.NewService(repo, 24*time.Hour)}
}

func newEvent(kind string, requester uuid.UUID, courier *uuid.UUID, errand uuid.UUID, at time.Time) notification.Event {
	return notification.Event{
		ID: uuid.New(), Type: kind, ErrandID: errand, RequesterID: requester,
		CourierID: courier, Amount: 5, OccurredAt: at,
	}
}

func (e *env) count(t *testing.T, recipient uuid.UUID) int {
	t.Helper()
	page, err := e.repo.List(t.Context(), recipient, notification.MaxLimit, 0)
	require.NoError(t, err)
	return page.Total
}

func (e *env) testRedelivery(t *testing.T) {
	requester, courier, errand := uuid.New(), uuid.New(), uuid.New()
	ev := newEvent(notification.EventCompleted, requester, &courier, errand, time.Now())
	for range 3 {
		require.NoError(t, e.svc.Handle(t.Context(), ev))
	}
	assert.Equal(t, 1, e.count(t, requester), "F5.5.2")
	assert.Equal(t, 1, e.count(t, courier), "F5.5.2")
}

func (e *env) testAtomic(t *testing.T) {
	recipient, errand := uuid.New(), uuid.New()
	eventID := uuid.New()
	good := notification.Notification{
		ID: uuid.New(), RecipientID: recipient, ErrandID: errand, EventID: eventID,
		StateChange: notification.EventAccepted, Message: "first",
	}
	// Reusing the id breaks the primary key, so the transaction must roll back.
	clash := good
	clash.RecipientID = uuid.New()
	require.Error(t, e.repo.Apply(t.Context(), nil, []notification.Notification{good, clash}))
	assert.Zero(t, e.count(t, recipient))
}

func (e *env) testOutOfOrderCourier(t *testing.T) {
	errand, first, second := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	require.NoError(t, e.repo.Apply(t.Context(), &notification.CourierChange{ErrandID: errand, CourierID: &second, ChangedAt: now}, nil))
	require.NoError(t, e.repo.Apply(t.Context(), &notification.CourierChange{ErrandID: errand, CourierID: &first, ChangedAt: now.Add(-time.Minute)}, nil))
	got, err := e.repo.CurrentCourier(t.Context(), errand)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, second, *got)

	require.NoError(t, e.repo.Apply(t.Context(), &notification.CourierChange{ErrandID: errand, ChangedAt: now.Add(time.Minute)}, nil))
	got, err = e.repo.CurrentCourier(t.Context(), errand)
	require.NoError(t, err)
	assert.Nil(t, got, "a withdrawal clears the courier")

	got, err = e.repo.CurrentCourier(t.Context(), uuid.New())
	require.NoError(t, err)
	assert.Nil(t, got, "an errand never seen has no courier")
}

func (e *env) testCancelReachesCourier(t *testing.T) {
	requester, courier, errand := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	require.NoError(t, e.svc.Handle(t.Context(), newEvent(notification.EventAccepted, requester, &courier, errand, now)))
	cancel := newEvent(notification.EventCancelled, requester, nil, errand, now.Add(time.Minute))
	require.NoError(t, e.svc.Handle(t.Context(), cancel))
	require.NoError(t, e.svc.Handle(t.Context(), cancel), "a redelivered cancel still finds the same courier")
	assert.Equal(t, 1, e.count(t, courier))
	assert.Equal(t, 2, e.count(t, requester))
}

func (e *env) testList(t *testing.T) {
	requester, errand := uuid.New(), uuid.New()
	var want []uuid.UUID
	for range 5 {
		ev := newEvent(notification.EventPickedUp, requester, nil, errand, time.Now())
		require.NoError(t, e.svc.Handle(t.Context(), ev))
		want = append([]uuid.UUID{ev.ID}, want...)
	}
	require.NoError(t, e.svc.Handle(t.Context(), newEvent(notification.EventExpired, uuid.New(), nil, errand, time.Now())))

	first, err := e.svc.List(t.Context(), requester, 3, 0)
	require.NoError(t, err)
	second, err := e.svc.List(t.Context(), requester, 3, 3)
	require.NoError(t, err)
	assert.Equal(t, 5, first.Total)
	require.Len(t, first.Notifications, 3)
	require.Len(t, second.Notifications, 2)

	var got []uuid.UUID
	for _, n := range append(first.Notifications, second.Notifications...) {
		assert.Equal(t, requester, n.RecipientID, "F5.1.2")
		assert.Equal(t, errand, n.ErrandID)
		assert.Equal(t, notification.EventPickedUp, n.StateChange)
		assert.False(t, n.Read)
		assert.False(t, n.CreatedAt.IsZero())
		got = append(got, n.EventID)
	}
	assert.Equal(t, want, got, "F5.1.3")
}

func (e *env) testMarkRead(t *testing.T) {
	requester := uuid.New()
	require.NoError(t, e.svc.Handle(t.Context(), newEvent(notification.EventExpired, requester, nil, uuid.New(), time.Now())))
	page, err := e.svc.List(t.Context(), requester, 1, 0)
	require.NoError(t, err)
	id := page.Notifications[0].ID

	require.ErrorIs(t, e.svc.MarkRead(t.Context(), uuid.New(), id), notification.ErrNotFound)
	require.ErrorIs(t, e.svc.MarkRead(t.Context(), requester, uuid.New()), notification.ErrNotFound)
	page, err = e.svc.List(t.Context(), requester, 1, 0)
	require.NoError(t, err)
	assert.False(t, page.Notifications[0].Read)

	require.NoError(t, e.svc.MarkRead(t.Context(), requester, id))
	require.NoError(t, e.svc.MarkRead(t.Context(), requester, id))
	page, err = e.svc.List(t.Context(), requester, 1, 0)
	require.NoError(t, err)
	assert.True(t, page.Notifications[0].Read)
}

type fixedToken struct{ user uuid.UUID }

func (f fixedToken) Verify(context.Context, string) (auth.Claims, error) {
	return auth.Claims{UserID: f.user, Role: "STUDENT"}, nil
}

type notRevoked struct{}

func (notRevoked) Revoked(context.Context, auth.Claims) (bool, error) { return false, nil }

func (e *env) testHTTP(t *testing.T) {
	user := uuid.New()
	require.NoError(t, e.svc.Handle(t.Context(), newEvent(notification.EventExpired, user, nil, uuid.New(), time.Now())))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.NewRouter(e.svc, e.pool, fixedToken{user}, notRevoked{}, log))
	t.Cleanup(srv.Close)

	call := func(method, path string) *http.Response {
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer a-token")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	health := call(http.MethodGet, "/health")
	assert.Equal(t, http.StatusOK, health.StatusCode)

	list := call(http.MethodGet, "/notifications")
	require.Equal(t, http.StatusOK, list.StatusCode)
	var page gen.NotificationPage
	require.NoError(t, json.NewDecoder(list.Body).Decode(&page))
	require.Len(t, page.Notifications, 1)
	assert.Equal(t, 1, page.Total)
	assert.False(t, page.Notifications[0].Read)

	read := call(http.MethodPost, "/notifications/"+page.Notifications[0].Id.String()+"/read")
	assert.Equal(t, http.StatusNoContent, read.StatusCode)

	again := call(http.MethodGet, "/notifications")
	require.NoError(t, json.NewDecoder(again.Body).Decode(&page))
	assert.True(t, page.Notifications[0].Read)

	missing := call(http.MethodPost, "/notifications/"+uuid.NewString()+"/read")
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
}

// testDownMigration runs last: it drops the tables the other tests use.
func (e *env) testDownMigration(t *testing.T) {
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_create_notification_tables.down.sql"))
	require.NoError(t, err)
	_, err = e.pool.Exec(t.Context(), string(sql))
	require.NoError(t, err)
	var left int
	require.NoError(t, e.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('notifications', 'errand_couriers')`).Scan(&left))
	assert.Zero(t, left)
}
