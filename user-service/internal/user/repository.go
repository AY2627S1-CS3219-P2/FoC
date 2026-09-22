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

// UserRepository persists users without exposing the underlying database adapter.
type UserRepository interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, uid uuid.UUID) (*User, error)
	GetByIdentifier(ctx context.Context, identifier string) (*User, error)
	// Update changes mutable user fields; UID and email are immutable.
	Update(ctx context.Context, u *User) error
	UpdateAccountStatusByID(ctx context.Context, uid uuid.UUID, status AccountStatus, tokensValidAfter time.Time) error
}

// SuspensionWriter persists the invalidation marker for a suspended account.
type SuspensionWriter interface {
	WriteSuspension(ctx context.Context, uid uuid.UUID, suspendedAt time.Time, ttl time.Duration) error
}

// SessionRepository persists refresh-token sessions and their revocation state.
type SessionRepository interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByHash(ctx context.Context, hash string) (*Session, error)
	RotateSession(ctx context.Context, oldHash string, newSession *Session) error
	RevokeSessionByHash(ctx context.Context, hash string) error
	RevokeAllUserSessions(ctx context.Context, uid uuid.UUID) error
}
