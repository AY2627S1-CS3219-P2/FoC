// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for PostgreSQL session row mapping and locking contract.
// Author review: COMPLETED BY ZI YANG

package repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sessionRow struct {
	values []any
	err    error
}

func (r sessionRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for index := range dest {
		switch value := dest[index].(type) {
		case *uuid.UUID:
			*value = r.values[index].(uuid.UUID)
		case *time.Time:
			*value = r.values[index].(time.Time)
		case **time.Time:
			if r.values[index] != nil {
				got := r.values[index].(time.Time)
				*value = &got
			}
		case *string:
			*value = r.values[index].(string)
		case **string:
			if r.values[index] != nil {
				got := r.values[index].(string)
				*value = &got
			}
		}
	}
	return nil
}

func TestScanSessionMapsRecordedFields(t *testing.T) {
	createdAt := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(7 * 24 * time.Hour)
	uid := uuid.New()
	jti := uuid.New()
	got, err := scanSession(sessionRow{values: []any{
		jti,
		createdAt,
		expiresAt,
		nil,
		"refresh-hash",
		nil,
		uid,
	}})
	if err != nil {
		t.Fatalf("scanSession() error = %v", err)
	}
	if got.JTI != jti || got.UID != uid || got.TokenHash != "refresh-hash" {
		t.Fatalf("session identifiers = %#v, want jti=%v uid=%v hash=refresh-hash", got, jti, uid)
	}
	if !got.CreatedAt.Equal(createdAt) || !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("session times = %v/%v, want %v/%v", got.CreatedAt, got.ExpiresAt, createdAt, expiresAt)
	}
	if got.RevokedAt != nil || got.ReplacedByTokenHash != nil {
		t.Fatalf("new session revocation fields = %#v, want nil", got)
	}
}

func TestMapSessionDatabaseErrorDistinguishesNotFound(t *testing.T) {
	if got := mapSessionDatabaseError(pgx.ErrNoRows); !errors.Is(got, user.ErrSessionNotFound) {
		t.Fatalf("mapSessionDatabaseError() = %v, want ErrSessionNotFound", got)
	}
}

func TestGetSessionByHashQueryLocksRow(t *testing.T) {
	if !strings.Contains(getSessionByHashQuery, "FOR UPDATE") {
		t.Fatalf("getSessionByHashQuery must lock the session row with FOR UPDATE")
	}
}
