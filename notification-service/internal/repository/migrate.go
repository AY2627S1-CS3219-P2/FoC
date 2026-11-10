// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Applies the migrations at startup, as in chat-service and order-service.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file" // the file:// migration source
	_ "github.com/jackc/pgx/v5/stdlib"                   // the pgx/v5 database/sql driver
)

const migrationsDatabaseName = "notifications"

// Migrate applies all pending migrations in migrationsDir to the database at
// databaseURL.
func Migrate(databaseURL, migrationsDir string) error {
	dir, err := filepath.Abs(migrationsDir)
	if err != nil {
		return fmt.Errorf("resolve migrations directory %q: %w", migrationsDir, err)
	}
	db, err := sql.Open("pgx/v5", databaseURL)
	if err != nil {
		return fmt.Errorf("open database for migrations: %w", err)
	}
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("prepare migration driver: %w", err)
	}
	source := (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}).String()
	m, err := migrate.NewWithDatabaseInstance(source, migrationsDatabaseName, driver)
	if err != nil {
		_ = driver.Close()
		return fmt.Errorf("load migrations from %s: %w", dir, err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations from %s: %w", dir, err)
	}
	return nil
}
