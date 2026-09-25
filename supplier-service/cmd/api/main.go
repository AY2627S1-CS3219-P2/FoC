package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"foc/supplier-service/internal/config"
	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/repository"
	"foc/supplier-service/internal/supplier"
	"foc/supplier-service/seed"
)

func main() {
	cfg := config.Load()
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

	if err := seed.EnsureSeeded(ctx, repo, cfg.SeedCSVPath); err != nil {
		log.Fatalf("seed suppliers: %v", err)
	}

	svc := supplier.NewService(repo)
	router := httpapi.NewRouter(svc)

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
