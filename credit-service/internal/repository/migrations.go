// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-12
// Scope: Generated startup migration runner for credit-service PostgreSQL schema.
// Author review: PENDING - reviewer to complete

package repository

import (
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

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
