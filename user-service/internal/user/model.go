// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the recorded user persistence model and account enumerations.
// Author review: COMPLETE BY ZI YANG

package user

import (
	"time"

	"github.com/google/uuid"
)

type AccountRole string

const (
	AccountRoleStudent AccountRole = "STUDENT"
	AccountRoleAdmin   AccountRole = "ADMIN"
)

// AccountStatus values match the account_status database enum.
type AccountStatus string

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
