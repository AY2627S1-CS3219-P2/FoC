// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Builds the token verifier and the Redis revocation client from
//   JWKS_URL and REDIS_URL and hands them to the router (D-041, D-054).
// Author review: PENDING — <reviewer to complete>

package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"foc/supplier-service/internal/auth"
	"foc/supplier-service/internal/config"
	"foc/supplier-service/internal/httpapi"
	// AI-generated (edited by James Teo).
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/repository"
	"foc/supplier-service/internal/supplier"
	"foc/supplier-service/seed"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	if err := runMigrations(cfg.DatabaseURL); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	repo := repository.NewPostgresRepository(pool)
	// AI-generated (edited by James Teo).
	errands := orderclient.New(cfg.OrderBaseURL)
	svc := supplier.NewService(repo, errands)

	// AI-generated (edited by James Teo).
	if err := seed.EnsureSeeded(ctx, repo, svc, cfg.SeedCSVPath); err != nil {
		log.Fatalf("seed suppliers: %v", err)
	}

	// Neither user-service nor Redis is waited for here: the keys are fetched
	// on the first admin write and go-redis connects lazily.
	verifier := auth.NewVerifier(cfg.JWKSURL, &http.Client{Timeout: 5 * time.Second})
	rdb, err := auth.NewRedisClient(cfg.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	defer rdb.Close()

	router := httpapi.NewRouter(svc, verifier, auth.NewRevocations(rdb))

	log.Printf("supplier-service listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runMigrations(databaseURL string) error {
	migrationsPath, err := filepath.Abs("migrations")
	if err != nil {
		return err
	}
	if _, err := os.Stat(migrationsPath); err != nil {
		migrationsPath = "/app/migrations"
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "supplier", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
