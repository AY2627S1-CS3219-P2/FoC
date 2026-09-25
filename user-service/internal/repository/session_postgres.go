// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added the PostgreSQL session repository with row locking and rotation transactions.
// Author review: COMPLETED BY ZI YANG

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	insertSessionQuery = `
		INSERT INTO sessions (
			jti, created_at, expires_at, revoked_at, token_hash,
			replaced_by_token_hash, uid
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	getSessionByHashQuery = `
		SELECT jti, created_at, expires_at, revoked_at, token_hash,
			replaced_by_token_hash, uid
		FROM sessions
		WHERE token_hash = $1
		FOR UPDATE`
	updateRotatedSessionQuery = `
		UPDATE sessions
		SET revoked_at = $1,
			replaced_by_token_hash = $2
		WHERE token_hash = $3
		  AND revoked_at IS NULL
		  AND replaced_by_token_hash IS NULL`
	updateRevokedSessionQuery = `
		UPDATE sessions
		SET revoked_at = $1
		WHERE token_hash = $2
		  AND revoked_at IS NULL`
	revokeAllUserSessionsQuery = `
		UPDATE sessions
		SET revoked_at = $1
		WHERE uid = $2
		  AND revoked_at IS NULL`
)

// PostgresSessionRepository persists refresh-token sessions in PostgreSQL.
type PostgresSessionRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresSessionRepository constructs a PostgreSQL session repository.
func NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{pool: pool}
}

// CreateSession stores a newly issued refresh-token session.
func (r *PostgresSessionRepository) CreateSession(ctx context.Context, session *user.Session) error {
	if session == nil {
		return errors.New("session is nil")
	}
	_, err := r.pool.Exec(ctx, insertSessionQuery,
		session.JTI,
		session.CreatedAt,
		session.ExpiresAt,
		session.RevokedAt,
		session.TokenHash,
		session.ReplacedByTokenHash,
		session.UID,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", mapSessionDatabaseError(err))
	}
	return nil
}

// GetSessionByHash retrieves a session while taking a row lock for refresh
// validation. The lock is held for the duration of this database transaction.
func (r *PostgresSessionRepository) GetSessionByHash(ctx context.Context, hash string) (*user.Session, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin get session transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	session, err := scanSession(tx.QueryRow(ctx, getSessionByHashQuery, hash))
	if err != nil {
		return nil, fmt.Errorf("get session by hash: %w", mapSessionDatabaseError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit get session transaction: %w", err)
	}
	return session, nil
}

// RotateSession atomically revokes an active session and inserts its replacement.
func (r *PostgresSessionRepository) RotateSession(ctx context.Context, oldHash string, newSession *user.Session) error {
	if newSession == nil {
		return errors.New("new session is nil")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rotate session transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := scanSession(tx.QueryRow(ctx, getSessionByHashQuery, oldHash)); err != nil {
		return fmt.Errorf("lock session for rotation: %w", mapSessionDatabaseError(err))
	}

	result, err := tx.Exec(ctx, updateRotatedSessionQuery, time.Now(), newSession.TokenHash, oldHash)
	if err != nil {
		return fmt.Errorf("revoke rotated session: %w", mapSessionDatabaseError(err))
	}
	if result.RowsAffected() == 0 {
		return user.ErrSessionCompromised
	}
	if _, err := tx.Exec(ctx, insertSessionQuery,
		newSession.JTI,
		newSession.CreatedAt,
		newSession.ExpiresAt,
		newSession.RevokedAt,
		newSession.TokenHash,
		newSession.ReplacedByTokenHash,
		newSession.UID,
	); err != nil {
		return fmt.Errorf("insert rotated session: %w", mapSessionDatabaseError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rotate session transaction: %w", err)
	}
	return nil
}

// RevokeSessionByHash marks one refresh-token session as revoked.
func (r *PostgresSessionRepository) RevokeSessionByHash(ctx context.Context, hash string) error {
	result, err := r.pool.Exec(ctx, updateRevokedSessionQuery, time.Now(), hash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", mapSessionDatabaseError(err))
	}
	if result.RowsAffected() == 0 {
		return user.ErrSessionNotFound
	}
	return nil
}

// RevokeAllUserSessions marks all active sessions for an account as revoked.
func (r *PostgresSessionRepository) RevokeAllUserSessions(ctx context.Context, uid uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, revokeAllUserSessionsQuery, time.Now(), uid); err != nil {
		return fmt.Errorf("revoke all user sessions: %w", mapSessionDatabaseError(err))
	}
	return nil
}

func scanSession(row rowScanner) (*user.Session, error) {
	got := new(user.Session)
	err := row.Scan(
		&got.JTI,
		&got.CreatedAt,
		&got.ExpiresAt,
		&got.RevokedAt,
		&got.TokenHash,
		&got.ReplacedByTokenHash,
		&got.UID,
	)
	if err != nil {
		return nil, err
	}
	return got, nil
}

func mapSessionDatabaseError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return user.ErrSessionNotFound
	}
	return err
}
