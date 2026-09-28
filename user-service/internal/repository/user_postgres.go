// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the PostgreSQL adapter for the recorded user repository contract.
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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	insertUserQuery = `
		INSERT INTO users (
			uid, username, email, password, phone_num, date_created,
			last_login_date, account_role, account_status, tokens_valid_after
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	getUserByIDQuery = `
		SELECT uid, username, email, password, phone_num, date_created,
			last_login_date, account_role, account_status, tokens_valid_after
		FROM users
		WHERE uid = $1`
	// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — matches the
	// recorded case-insensitive identifier policy. Author review: ZI YANG - validated correctness.
	getUserByIdentifierQuery = `
		SELECT uid, username, email, password, phone_num, date_created,
			last_login_date, account_role, account_status, tokens_valid_after
		FROM users
		WHERE LOWER(username) = LOWER($1) OR LOWER(email) = LOWER($1)`
	hasAdminQuery = `
		SELECT EXISTS (
			SELECT 1 FROM users WHERE account_role = 'ADMIN'
		)`
	countActiveAdminsQuery = `
		SELECT COUNT(*) FROM users
		WHERE account_role = 'ADMIN' AND account_status = 'ACTIVE'`
	updateUserQuery = `
		UPDATE users
		SET username = $1,
			password = $2,
			phone_num = $3,
			tokens_valid_after = $4
		WHERE uid = $5`
	updateLastLoginQuery = `UPDATE users SET last_login_date = $1 WHERE uid = $2`
	// AI-generated (edited by ZI YANG).
	updateAccountStatusQuery = `
		UPDATE users
		SET account_status = $1::account_status,
			tokens_valid_after = CASE
				WHEN $1::account_status = 'SUSPENDED' THEN $2
				ELSE tokens_valid_after
			END
		WHERE uid = $3`
)

// AI-generated (edited by ZI YANG): documents the exported repository adapter
// and makes the suspension invalidation boundary explicit.
// PostgresRepository stores user accounts in PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository returns a PostgresRepository that uses pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserts a user and fills Go-generated fields that were not supplied.
func (r *PostgresRepository) Create(ctx context.Context, u *user.User) error {
	prepared := prepareForCreate(*u, time.Now())
	_, err := r.pool.Exec(ctx, insertUserQuery,
		prepared.UID,
		prepared.Username,
		prepared.Email,
		prepared.PasswordHash,
		prepared.PhoneNum,
		prepared.DateCreated,
		prepared.LastLoginDate,
		prepared.AccountRole,
		prepared.AccountStatus,
		prepared.TokensValidAfter,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", mapDatabaseError(err))
	}
	*u = prepared
	return nil
}

// GetByID retrieves a user by UUID.
func (r *PostgresRepository) GetByID(ctx context.Context, uid uuid.UUID) (*user.User, error) {
	return r.get(ctx, r.pool.QueryRow(ctx, getUserByIDQuery, uid), "get user by id")
}

// GetByIdentifier retrieves a user by username or email.
func (r *PostgresRepository) GetByIdentifier(ctx context.Context, identifier string) (*user.User, error) {
	return r.get(ctx, r.pool.QueryRow(ctx, getUserByIdentifierQuery, identifier), "get user by identifier")
}

// HasAdmin reports whether the users table already contains an administrator.
func (r *PostgresRepository) HasAdmin(ctx context.Context) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, hasAdminQuery).Scan(&exists); err != nil {
		return false, fmt.Errorf("check for admin: %w", err)
	}
	return exists, nil
}

// CountActiveAdmins reports the number of active administrator accounts.
func (r *PostgresRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — implements the
	// recorded last-active-admin guard query. Author review: ZI YANG - validated correctness.
	if err := r.pool.QueryRow(ctx, countActiveAdminsQuery).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active admins: %w", err)
	}
	return count, nil
}

// Update updates mutable user fields without changing UID or email.
func (r *PostgresRepository) Update(ctx context.Context, u *user.User) error {
	result, err := r.pool.Exec(ctx, updateUserQuery,
		u.Username,
		u.PasswordHash,
		u.PhoneNum,
		u.TokensValidAfter,
		u.UID,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", mapDatabaseError(err))
	}
	if result.RowsAffected() == 0 {
		return user.ErrNotFound
	}
	return nil
}

// UpdateLastLoginByID records successful authentication without modifying profile or status fields.
func (r *PostgresRepository) UpdateLastLoginByID(ctx context.Context, uid uuid.UUID, timestamp time.Time) error {
	result, err := r.pool.Exec(ctx, updateLastLoginQuery, timestamp, uid)
	if err != nil {
		return fmt.Errorf("update last login: %w", mapDatabaseError(err))
	}
	if result.RowsAffected() == 0 {
		return user.ErrNotFound
	}
	return nil
}

// UpdateAccountStatusByID sets the account status. Suspending also sets
// tokens_valid_after, so refresh tokens issued before it are rejected;
// reactivating leaves it unchanged. The caller passes the timestamp so it
// matches the suspension key written to Redis.
func (r *PostgresRepository) UpdateAccountStatusByID(ctx context.Context, uid uuid.UUID, status user.AccountStatus, tokensValidAfter time.Time) error {
	result, err := r.pool.Exec(ctx, updateAccountStatusQuery, status, tokensValidAfter, uid)
	if err != nil {
		return fmt.Errorf("update account status: %w", mapDatabaseError(err))
	}
	if result.RowsAffected() == 0 {
		return user.ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PostgresRepository) get(_ context.Context, row rowScanner, operation string) (*user.User, error) {
	got, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, mapDatabaseError(err))
	}
	return got, nil
}

func scanUser(row rowScanner) (*user.User, error) {
	got := new(user.User)
	err := row.Scan(
		&got.UID,
		&got.Username,
		&got.Email,
		&got.PasswordHash,
		&got.PhoneNum,
		&got.DateCreated,
		&got.LastLoginDate,
		&got.AccountRole,
		&got.AccountStatus,
		&got.TokensValidAfter,
	)
	if err != nil {
		return nil, err
	}
	return got, nil
}

func prepareForCreate(u user.User, now time.Time) user.User {
	if u.UID == uuid.Nil {
		u.UID = uuid.New()
	}
	if u.DateCreated.IsZero() {
		u.DateCreated = now.In(time.FixedZone("SGT", 8*60*60))
	}
	if u.AccountRole == "" {
		u.AccountRole = user.AccountRoleStudent
	}
	if u.AccountStatus == "" {
		u.AccountStatus = user.AccountStatusActive
	}
	if u.TokensValidAfter.IsZero() {
		// AI-generated (edited by ZI YANG).
		u.TokensValidAfter = now.Truncate(time.Second)
	}
	return u
}

func mapDatabaseError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return user.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — maps the
		// recorded case-insensitive index names to existing domain errors.
		// Author review: ZI YANG - validated correctness.
		case "idx_users_email_lower":
			return user.ErrDuplicateEmail
		case "idx_users_username_lower":
			return user.ErrDuplicateUsername
		}
	}
	return err
}
