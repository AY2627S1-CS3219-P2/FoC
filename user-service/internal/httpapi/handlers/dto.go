// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated HTTP request and response DTOs with their handlers.
// Author review: COMPLETED BY ZI YANG

// Package handlers contains grouped HTTP endpoint handlers and their transport dependencies.
package handlers

import (
	"time"

	"foc/user-service/internal/user"
)

// RegisterRequest is the JSON body accepted by registration.
type RegisterRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type OTPEmailRequest struct {
	Email string `json:"email"`
}

type VerifyOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// LoginRequest is the JSON body accepted by login.
type LoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// RefreshRequest is the JSON body accepted by refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// LogoutRequest is the JSON body accepted by logout.
type LogoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// AuthResponse is the JSON response containing an access and refresh token.
type AuthResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// AI-generated (edited by ZI YANG): distinguishes the full profile from the
// restricted response used for non-admin callers.
// UserResponse is the full profile, including role, status, and creation date.
// Profile returns it only to admins; UpdateProfile returns it to the caller.
type UserResponse struct {
	UID           string    `json:"uid"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	PhoneNum      string    `json:"phone_num"`
	AccountRole   string    `json:"account_role"`
	AccountStatus string    `json:"account_status"`
	DateCreated   time.Time `json:"date_created"`
}

// AI-generated (edited by ZI YANG): restricted profile response for non-admin callers.
// RestrictedUserResponse is the profile visible to authenticated non-admin users.
type RestrictedUserResponse struct {
	UID      string `json:"uid"`
	Username string `json:"username"`
	Email    string `json:"email"`
	PhoneNum string `json:"phone_num"`
}

// UpdateStatusRequest is the JSON body accepted by account-status updates.
type UpdateStatusRequest struct {
	Status user.AccountStatus `json:"status"`
}

// UpdateProfileRequest is the JSON body accepted by profile updates.
type UpdateProfileRequest struct {
	Username string `json:"username"`
	// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — transcribes the
	// recorded password-change request contract. Author review: ZI YANG.
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	PhoneNum        string `json:"phone_num"`
}

// UserListItem is one row of the admin's account list (D-122): the full profile
// plus the last login, for admins only.
type UserListItem struct {
	UID           string     `json:"uid"`
	Username      string     `json:"username"`
	Email         string     `json:"email"`
	PhoneNum      string     `json:"phone_num"`
	AccountRole   string     `json:"account_role"`
	AccountStatus string     `json:"account_status"`
	DateCreated   time.Time  `json:"date_created"`
	LastLoginDate *time.Time `json:"last_login_date"`
}

// UserPageResponse is one page of the account list with the total that matched,
// for "page 3 of N" (D-104).
type UserPageResponse struct {
	Items  []UserListItem `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// AuditEntryResponse is one audit log row as the API returns it (D-113).
type AuditEntryResponse struct {
	ID                 string         `json:"id"`
	OccurredAt         time.Time      `json:"occurred_at"`
	ActorID            string         `json:"actor_id"`
	ActorRole          string         `json:"actor_role"`
	Action             string         `json:"action"`
	TargetType         string         `json:"target_type"`
	TargetID           string         `json:"target_id"`
	Outcome            string         `json:"outcome"`
	RefusalReason      string         `json:"refusal_reason"`
	Reason             string         `json:"reason"`
	BeforeState        map[string]any `json:"before_state"`
	AfterState         map[string]any `json:"after_state"`
	AdditionalComments map[string]any `json:"additional_comments"`
	CorrelationID      string         `json:"correlation_id"`
}

// AuditPageResponse is one page of the audit log with the total that matched.
type AuditPageResponse struct {
	Items  []AuditEntryResponse `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}
