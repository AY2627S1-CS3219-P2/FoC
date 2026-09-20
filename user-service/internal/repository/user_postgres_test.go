// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added focused tests for PostgreSQL row/error mapping and Go-side defaults.
// Author review: PENDING — reviewer to complete

package repository

import (
	"errors"
	"testing"
	"time"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPrepareForCreateGeneratesRecordedDefaults(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	got := prepareForCreate(user.User{}, now)

	if got.UID == uuid.Nil {
		t.Fatal("UID was not generated")
	}
	if got.DateCreated.Location().String() != "SGT" {
		t.Fatalf("DateCreated location = %q, want SGT", got.DateCreated.Location())
	}
	if got.AccountRole != user.AccountRoleStudent {
		t.Fatalf("AccountRole = %q, want %q", got.AccountRole, user.AccountRoleStudent)
	}
	if got.AccountStatus != user.AccountStatusActive {
		t.Fatalf("AccountStatus = %q, want %q", got.AccountStatus, user.AccountStatusActive)
	}
}

func TestMapDatabaseErrorDistinguishesNotFound(t *testing.T) {
	if got := mapDatabaseError(pgx.ErrNoRows); !errors.Is(got, user.ErrNotFound) {
		t.Fatalf("mapDatabaseError() = %v, want ErrNotFound", got)
	}
}

func TestMapDatabaseErrorDistinguishesDuplicateEmail(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}
	if got := mapDatabaseError(pgErr); !errors.Is(got, user.ErrDuplicateEmail) {
		t.Fatalf("mapDatabaseError() = %v, want ErrDuplicateEmail", got)
	}
}

func TestMapDatabaseErrorDistinguishesDuplicateUsername(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "users_username_key"}
	if got := mapDatabaseError(pgErr); !errors.Is(got, user.ErrDuplicateUsername) {
		t.Fatalf("mapDatabaseError() = %v, want ErrDuplicateUsername", got)
	}
}

func TestMapDatabaseErrorPreservesUnknownErrors(t *testing.T) {
	want := errors.New("database unavailable")
	if got := mapDatabaseError(want); !errors.Is(got, want) {
		t.Fatalf("mapDatabaseError() = %v, want wrapped original error", got)
	}
}
