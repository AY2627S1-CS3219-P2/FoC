// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the recorded user persistence model and account enumerations.
// Author review: COMPLETE BY ZI YANG

package user

import (
	"time"

	"github.com/google/uuid"
)

// AI-generated (edited by ZI YANG): adds exported role and status documentation
// tied to the persisted enum values without changing the model.
// AccountRole is an account's permission level, carried in the access token's
// role claim.
type AccountRole string

// Account roles. Each value must match the account_role database enum.
const (
	AccountRoleStudent AccountRole = "STUDENT"
	AccountRoleAdmin   AccountRole = "ADMIN"
)

// AccountStatus says whether an account may sign in (ACTIVE) or not
// (SUSPENDED).
type AccountStatus string

// Account statuses. Each value must match the account_status database enum.
const (
	AccountStatusActive    AccountStatus = "ACTIVE"
	AccountStatusSuspended AccountStatus = "SUSPENDED"
)

// User is an account as stored in the users table.
type User struct {
	UID              uuid.UUID
	Username         string
	Email            string
	PasswordHash     string
	PhoneNum         string
	DateCreated      time.Time
	LastLoginDate    *time.Time
	AccountRole      AccountRole
	AccountStatus    AccountStatus
	TokensValidAfter time.Time
}

// Session stores the hashed refresh-token state owned by user-service.
type Session struct {
	JTI                 uuid.UUID  `json:"jti"`
	CreatedAt           time.Time  `json:"created_at"`
	ExpiresAt           time.Time  `json:"expires_at"`
	RevokedAt           *time.Time `json:"revoked_at"`
	TokenHash           string     `json:"-"`
	ReplacedByTokenHash *string    `json:"-"`
	UID                 uuid.UUID  `json:"uid"`
}
