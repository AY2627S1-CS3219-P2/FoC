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

// UserResponse is the JSON response containing a public profile.
type UserResponse struct {
	UID           string    `json:"uid"`
	Username      string    `json:"username"`
	AccountRole   string    `json:"account_role"`
	AccountStatus string    `json:"account_status"`
	DateCreated   time.Time `json:"date_created"`
}

// UpdateStatusRequest is the JSON body accepted by account-status updates.
type UpdateStatusRequest struct {
	Status user.AccountStatus `json:"status"`
}

// UpdateProfileRequest is the JSON body accepted by profile updates.
type UpdateProfileRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
