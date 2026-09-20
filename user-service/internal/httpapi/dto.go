// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added DTOs transcribed from the recorded refresh OpenAPI contract.
// Author review: COMPLETED BY ZI YANG

package httpapi

import (
	"foc/user-service/internal/user"
	"time"
)

// RegisterRequest is the JSON body accepted by the registration endpoint.
type RegisterRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginRequest is the JSON body accepted by the login endpoint.
type LoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// RefreshRequest is the JSON body accepted by the refresh endpoint.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// LogoutRequest is the JSON body accepted by the logout endpoint.
type LogoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// AuthResponse is the recorded camelCase JWT response shape.
type AuthResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// UserResponse is the recorded public-profile response shape.
type UserResponse struct {
	UID           string    `json:"uid"`
	Username      string    `json:"username"`
	AccountRole   string    `json:"account_role"`
	AccountStatus string    `json:"account_status"`
	DateCreated   time.Time `json:"date_created"`
}

type UpdateStatusRequest struct {
	Status user.AccountStatus `json:"status"`
}

type UpdateProfileRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
