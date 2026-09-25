// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the recorded user persistence model and account enumerations.
// Author review: COMPLETE BY ZI YANG

package user

import (
	"github.com/google/uuid"
	"time"
)

type AccountRole string

const (
	AccountRoleStudent AccountRole = "STUDENT"
	AccountRoleAdmin   AccountRole = "ADMIN"
)

type AccountStatus string

const (
	AccountStatusActive    AccountStatus = "ACTIVE"
	AccountStatusSuspended AccountStatus = "SUSPENDED"
)

type User struct {
	UID              uuid.UUID     `json:"uid"`
	Username         string        `json:"username"`
	Email            string        `json:"email"`
	PasswordHash     string        `json:"password"`
	PhoneNum         string        `json:"phone_num"`
	DateCreated      time.Time     `json:"date_created"`
	LastLoginDate    *time.Time    `json:"last_login_date"`
	AccountRole      AccountRole   `json:"account_role"`
	AccountStatus    AccountStatus `json:"account_status"`
	TokensValidAfter time.Time     `json:"-"`
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
