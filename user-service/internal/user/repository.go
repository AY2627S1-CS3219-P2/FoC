// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the recorded user repository interface.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AI-generated (edited by ZI YANG): keeps the repository documentation focused
// on its account persistence operations.
// UserRepository stores and looks up user accounts.
type UserRepository interface {
	HasAdmin(ctx context.Context) (bool, error)
	CountActiveAdmins(ctx context.Context) (int, error)
	Create(ctx context.Context, u *User) error
	CreateWithTask(ctx context.Context, u *User, task func() error) error
	GetByID(ctx context.Context, uid uuid.UUID) (*User, error)
	GetByIdentifier(ctx context.Context, identifier string) (*User, error)
	// Update changes mutable user fields; UID and email are immutable.
	Update(ctx context.Context, u *User) error
	UpdateLastLoginByID(ctx context.Context, uid uuid.UUID, timestamp time.Time) error
	UpdateAccountStatusByID(ctx context.Context, uid uuid.UUID, status AccountStatus, tokensValidAfter time.Time) error
	ActivateUser(ctx context.Context, email string) error
}

// OTPStore stores OTP hashes and tracks OTP request counts in Redis.
type OTPStore interface {
	IncrementRequestCount(ctx context.Context, email string) (int64, error)
	// AI-generated (edited by James Teo): StoreHash's reset and the two attempt
	// methods below back the per-code wrong-guess limit.
	// StoreHash replaces the stored hash and resets its attempt count.
	StoreHash(ctx context.Context, email, hash string) error
	LookupHash(ctx context.Context, email string) (string, bool, error)
	RequestCount(ctx context.Context, email string) (int64, bool, error)
	// IncrementAttemptCount counts one verification attempt against the stored
	// hash and returns the new count.
	IncrementAttemptCount(ctx context.Context, email string) (int64, error)
	// DeleteHash removes the stored hash and its attempt count, keeping the
	// request count.
	DeleteHash(ctx context.Context, email string) error
	// Delete removes the hash, the request count and the attempt count.
	Delete(ctx context.Context, email string) error
}

// OTPDeliverer sends a plaintext OTP only after its hash is durably stored.
type OTPDeliverer interface {
	DeliverOTP(ctx context.Context, email, otp string) error
}

// SuspensionWriter writes the invalidation marker to Redis for a suspended account.
type SuspensionWriter interface {
	WriteSuspension(ctx context.Context, uid uuid.UUID, suspendedAt time.Time, ttl time.Duration) error
}

// SessionRepository stores refresh-token sessions and their revocation state.
type SessionRepository interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByHash(ctx context.Context, hash string) (*Session, error)
	RotateSession(ctx context.Context, oldHash string, newSession *Session) error
	RevokeSessionByHash(ctx context.Context, hash string) error
	RevokeAllUserSessions(ctx context.Context, uid uuid.UUID) error
}
