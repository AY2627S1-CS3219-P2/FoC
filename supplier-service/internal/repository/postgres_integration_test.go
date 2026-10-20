//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Integration tests of the Postgres repository and the router over it
//   (root AGENTS.md §7): ten concurrent identical creates give one success and
//   nine ErrDuplicate, a unique violation on Update is ErrDuplicate, Update
//   after a soft delete is ErrNotFound, and through the router a PUT on an
//   upper-case id is 200 and a non-UUID GET is 404.
// Author review: PENDING — <reviewer to complete>

package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/repository"
	"foc/supplier-service/internal/supplier"
)

// noErrands is an ErrandCounter for tests that never delete.
type noErrands struct{}

func (noErrands) ActiveErrands(context.Context, string) (int, error) { return 0, nil }

func stall(name string) *supplier.Supplier {
	return &supplier.Supplier{
		Name: name, Type: "cafe", Building: "COM1", LocationDescription: "Level 2",
		Latitude: 1.29, Longitude: 103.77, IsAvailable: true,
	}
}

func TestRepositoryOverPostgres(t *testing.T) {
	t.Parallel()
	pool := openDatabase(t)
	repo := repository.NewPostgresRepository(pool)
	svc := supplier.NewService(repo, noErrands{})
	ctx := context.Background()

	t.Run("ten concurrent identical creates give one success and nine duplicates", func(t *testing.T) {
		// Each create passes ExistsDuplicate before any has inserted, so the
		// unique index is what refuses the rest.
		const n = 10
		results := make(chan error, n)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := svc.Create(ctx, stall("Racing Stall"))
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)

		var created, duplicates int
		for err := range results {
			switch {
			case err == nil:
				created++
			case errors.Is(err, supplier.ErrDuplicate):
				duplicates++
			default:
				t.Errorf("a racing create must be a success or ErrDuplicate, got %v", err)
			}
		}
		assert.Equal(t, 1, created)
		assert.Equal(t, n-1, duplicates)
	})

	t.Run("an update that collides on the unique index is ErrDuplicate", func(t *testing.T) {
		a, err := svc.Create(ctx, stall("Collide A"))
		require.NoError(t, err)
		b, err := svc.Create(ctx, stall("Collide B"))
		require.NoError(t, err)

		// Straight to the repository, skipping the service's duplicate check,
		// as a racing PUT would.
		b.Name = a.Name
		assert.ErrorIs(t, repo.Update(ctx, b), supplier.ErrDuplicate)
	})

	t.Run("an update after a soft delete is ErrNotFound", func(t *testing.T) {
		s, err := svc.Create(ctx, stall("Doomed Stall"))
		require.NoError(t, err)
		require.NoError(t, repo.SoftDelete(ctx, s.ID))

		s.Description = "changed after the delete"
		assert.ErrorIs(t, repo.Update(ctx, s), supplier.ErrNotFound, "a change that was not saved is not a success")
	})

	t.Run("through the router, a PUT on an upper-case id keeping its name is 200 and a non-UUID GET is 404", func(t *testing.T) {
		s, err := svc.Create(ctx, stall("Upper Stall"))
		require.NoError(t, err)
		router := httpapi.NewRouter(svc)

		body := `{"name":"Upper Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.29,"longitude":103.77,"is_available":true}`
		req := httptest.NewRequest(http.MethodPut, "/suppliers/"+strings.ToUpper(s.ID), strings.NewReader(body))
		req.Header.Set("X-User-Role", "ADMIN")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/suppliers/not-a-uuid", nil))
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/suppliers/"+uuid.NewString(), nil))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
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
