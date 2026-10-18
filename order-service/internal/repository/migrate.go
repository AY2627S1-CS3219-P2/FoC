// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Applies order-service's migrations (D-053) with golang-migrate's
//   pgx/v5 driver, as supplier-service's runMigrations does.
// Author review: James Teo — reviewed and verified.

package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file" // registers the file:// source
	_ "github.com/jackc/pgx/v5/stdlib"                   // registers the pgx/v5 database/sql driver
)

// migrationsDatabaseName is the name golang-migrate gives the database in its
// messages.
const migrationsDatabaseName = "orders"

// Migrate applies every pending migration in migrationsDir to the database at
// databaseURL. Finding nothing to apply is not an error. It does not wait for
// the database to come up: a caller starting alongside it retries.
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
		// The driver's Close also closes db.
		_ = driver.Close()
		return fmt.Errorf("load migrations from %s: %w", dir, err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations from %s: %w", dir, err)
	}
	return nil
}
