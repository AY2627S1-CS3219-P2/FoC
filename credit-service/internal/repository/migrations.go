// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added schema version verification and inspection per sourceOfTruth.md Phase 12.
// Author review: PENDING - reviewer to complete

package repository

import (
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// ExpectedSchemaVersion is the latest known database migration version supported by this release.
const ExpectedSchemaVersion uint = 1

// Apply runs all pending migrations from sourceURL against databaseURL.
func Apply(databaseURL, sourceURL string) error {
	migrator, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = migrator.Close()
	}()

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// GetSchemaVersion inspects the current database schema version and whether it is dirty.
func GetSchemaVersion(databaseURL, sourceURL string) (uint, bool, error) {
	migrator, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		_, _ = migrator.Close()
	}()

	version, dirty, err := migrator.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return version, dirty, nil
}
