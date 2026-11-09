// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Credential issuance boundary shared with the approved role-change locks.
// Author review: ZI YANG - reviewed and verified

package session

import (
	"context"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

// CredentialSessionRepository holds a shared account lock through issuance and
// session persistence. The callback receives a freshly read account and a
// transaction-bound session repository; credentials return only after commit.
type CredentialSessionRepository interface {
	user.SessionRepository
	WithAccountCredentials(ctx context.Context, uid uuid.UUID, issue func(*user.User, user.SessionRepository) error) error
}
