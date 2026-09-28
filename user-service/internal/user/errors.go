// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added repository errors that callers must distinguish.
// Author review: COMPLETE BY ZI YANG

package user

import "errors"

var (
	ErrNotFound           = errors.New("user not found")
	ErrDuplicateEmail     = errors.New("email address is already in use")
	ErrDuplicateUsername  = errors.New("username is already in use")
	ErrInvalidCredentials = errors.New("invalid credentials")
	// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — distinguishes the
	// recorded current-password failure from invalid new-password policy.
	// Author review: PENDING.
	ErrInvalidCurrentPassword = errors.New("invalid current password")
	ErrInvalidPassword        = errors.New("invalid password")
	ErrInvalidUsername        = errors.New("invalid username")
	ErrInvalidEmail           = errors.New("invalid email")
	ErrEmailTooLong           = errors.New("email exceeds maximum length")
	ErrInvalidPhone           = errors.New("invalid phone number")
	ErrAccountSuspended       = errors.New("account is suspended")
	ErrSessionNotFound        = errors.New("session not found")
	ErrSessionCompromised     = errors.New("session compromised")
)
