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
	UID           uuid.UUID     `json:"uid"`
	Username      string        `json:"username"`
	Email         string        `json:"email"`
	PasswordHash  string        `json:"password"`
	PhoneNum      string        `json:"phone_num"`
	DateCreated   time.Time     `json:"date_created"`
	LastLoginDate *time.Time    `json:"last_login_date"`
	AccountRole   AccountRole   `json:"account_role"`
	AccountStatus AccountStatus `json:"account_status"`
}
