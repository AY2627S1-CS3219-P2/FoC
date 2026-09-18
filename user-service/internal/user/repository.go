// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the recorded user repository interface.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"github.com/google/uuid"
)

// Repository persists users without exposing the underlying database adapter.
type Repository interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, uid uuid.UUID) (*User, error)
	GetByIdentifier(ctx context.Context, identifier string) (*User, error)
	// Update changes mutable user fields; UID and email are immutable.
	Update(ctx context.Context, u *User) error
}
