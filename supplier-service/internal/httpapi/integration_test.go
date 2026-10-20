//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Integration tests of the admin writes over a real PostgreSQL and a
//   real Redis (root AGENTS.md §7): create, update and delete with a fake
//   order-service answering 0, revocation keys written the way user-service
//   writes them, and a Redis that is stopped (D-041, D-054).
// Author review: PENDING — <reviewer to complete>
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: TestBadIDsAndCaseOverPostgres: through the router over a real
//   PostgreSQL, a PUT on an upper-case id keeping its name is 200 and a
//   non-UUID GET is 404 (#35).
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"foc/supplier-service/internal/auth"
	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/repository"
	"foc/supplier-service/internal/supplier"
)

func TestAdminWritesOverPostgresAndRedis(t *testing.T) {
	t.Parallel()
	pool := openDatabase(t)
	redisContainer, redisAddr := startRedis(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, DialTimeout: 500 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	// writer stands in for user-service, the revocation keys' only writer.
	writer := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = writer.Close() })
	_, orderSrv := startOrderService(t, http.StatusOK, `{"supplierId":"x","activeErrands":0}`)

	repo := repository.NewPostgresRepository(pool)
	router := httpapi.NewRouter(supplier.NewService(repo, orderclient.New(orderSrv.URL)),
		startKeySet(t, http.StatusOK), auth.NewRevocations(rdb))
	ctx := context.Background()

	create := func(authorization, name string) *httptest.ResponseRecorder {
		body := `{"name":"` + name + `","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.29,"longitude":103.77}`
		req := httptest.NewRequest(http.MethodPost, "/suppliers", strings.NewReader(body))
		req.Header.Set("Authorization", authorization)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("an admin creates, updates and deletes a supplier", func(t *testing.T) {
		admin := "Bearer " + mintToken(t, tokenSpec{})

		rec := create(admin, "Integration Stall")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		id := jsonField(t, rec, "id")

		body := `{"name":"Integration Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.29,"longitude":103.77,"is_available":false}`
		req := httptest.NewRequest(http.MethodPut, "/suppliers/"+id, strings.NewReader(body))
		req.Header.Set("Authorization", admin)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		req = httptest.NewRequest(http.MethodDelete, "/suppliers/"+id, nil)
		req.Header.Set("Authorization", admin)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	})

	t.Run("a jti key written the way user-service writes it refuses the token", func(t *testing.T) {
		jti := uuid.NewString()
		token := "Bearer " + mintToken(t, tokenSpec{jti: jti})
		require.Equal(t, http.StatusCreated, create(token, "Before Logout").Code)

		require.NoError(t, writer.Set(ctx, "jti:"+jti, "1", time.Minute).Err())

		rec := create(token, "After Logout")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})

	t.Run("a suspension key written the way user-service writes it refuses the token", func(t *testing.T) {
		sub := uuid.New()
		token := "Bearer " + mintToken(t, tokenSpec{sub: sub})
		require.Equal(t, http.StatusCreated, create(token, "Before Suspension").Code)

		suspendedAt := time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
		require.NoError(t, writer.Set(ctx, "suspended:uid:"+sub.String(), suspendedAt, time.Minute).Err())

		rec := create(token, "After Suspension")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})

	t.Run("with Redis stopped, writes get 503 and reads still work", func(t *testing.T) {
		timeout := 5 * time.Second
		require.NoError(t, redisContainer.Stop(ctx, &timeout))

		rec := create("Bearer "+mintToken(t, tokenSpec{}), "Redis Down")
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())

		req := httptest.NewRequest(http.MethodGet, "/suppliers", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}

func TestBadIDsAndCaseOverPostgres(t *testing.T) {
	t.Parallel()
	repo := repository.NewPostgresRepository(openDatabase(t))
	router := httpapi.NewRouter(supplier.NewService(repo, &zeroErrands{}), startKeySet(t, http.StatusOK), &fakeRevocations{})
	admin := "Bearer " + mintToken(t, tokenSpec{})
	serve := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", admin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	const body = `{"name":"Upper Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.29,"longitude":103.77,"is_available":true}`
	created := serve(http.MethodPost, "/suppliers", body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	id := jsonField(t, created, "id")

	t.Run("a PUT on an upper-case id keeping its name and location is 200", func(t *testing.T) {
		rec := serve(http.MethodPut, "/suppliers/"+strings.ToUpper(id), body)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("a GET on a non-UUID is 404, not a database error", func(t *testing.T) {
		rec := serve(http.MethodGet, "/suppliers/not-a-uuid", "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"error":"supplier not found"}`, rec.Body.String())
	})
}

// jsonField returns a top-level string field of a JSON response.
func jsonField(t *testing.T, rec *httptest.ResponseRecorder, field string) string {
	t.Helper()
	body := rec.Body.String()
	marker := `"` + field + `":"`
	i := strings.Index(body, marker)
	require.GreaterOrEqual(t, i, 0, "no %s in %s", field, body)
	rest := body[i+len(marker):]
	return rest[:strings.Index(rest, `"`)]
}

// openDatabase starts PostgreSQL, applies the migrations and returns a pool
// over it, closed when the test ends.
func openDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := startPostgres(t)
	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://../../migrations", "supplier", driver)
	require.NoError(t, err)
	require.NoError(t, m.Up(), "apply migrations")
	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// startPostgres starts postgres:16-alpine, the image Compose uses, and returns
// its URL.
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "supplier",
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
	return "postgres://postgres:postgres@" + net.JoinHostPort(host, port.Port()) + "/supplier?sslmode=disable"
}

// startRedis starts redis:7-alpine, the image Compose uses, and returns the
// container, so a test can stop it, and its address.
func startRedis(t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err, "start Redis container")
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379/tcp")
	require.NoError(t, err)
	return container, net.JoinHostPort(host, port.Port())
}
