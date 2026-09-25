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
	getUserByIdentifierQuery = `
		SELECT uid, username, email, password, phone_num, date_created,
			last_login_date, account_role, account_status, tokens_valid_after
		FROM users
		WHERE username = $1 OR email = $1`
	hasAdminQuery = `
		SELECT EXISTS (
			SELECT 1 FROM users WHERE account_role = 'ADMIN'
		)`
	updateUserQuery = `
		UPDATE users
		SET username = $1,
			password = $2,
			phone_num = $3,
			last_login_date = $4,
			account_role = $5,
			account_status = $6
		WHERE uid = $7`
	updateAccountStatusQuery = `
		UPDATE users
		SET account_status = $1,
			tokens_valid_after = CASE
				WHEN $1 = 'SUSPENDED' THEN $2
				ELSE tokens_valid_after
			END
		WHERE uid = $3`
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

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

// Update updates mutable user fields without changing UID or email.
func (r *PostgresRepository) Update(ctx context.Context, u *user.User) error {
	result, err := r.pool.Exec(ctx, updateUserQuery,
		u.Username,
		u.PasswordHash,
		u.PhoneNum,
		u.LastLoginDate,
		u.AccountRole,
		u.AccountStatus,
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

// UpdateAccountStatusByID changes account status and invalidates existing JWTs
// only when suspending an account. Reactivation preserves the validity boundary.
// The caller supplies the timestamp so it can be shared with Redis invalidation.
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
		u.TokensValidAfter = now
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
		case "users_email_key":
			return user.ErrDuplicateEmail
		case "users_username_key":
			return user.ErrDuplicateUsername
		}
	}
	return err
}
