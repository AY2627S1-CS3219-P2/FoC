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
	// Author review: ZI YANG - validated correctness.
	ErrInvalidCurrentPassword = errors.New("invalid current password")
	ErrInvalidPassword        = errors.New("invalid password")
	ErrInvalidUsername        = errors.New("invalid username")
	ErrInvalidEmail           = errors.New("invalid email")
	ErrEmailTooLong           = errors.New("email exceeds maximum length")
	ErrInvalidPhone           = errors.New("invalid phone number")
	ErrAccountSuspended       = errors.New("account is suspended")
	// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — distinguishes the
	// recorded last-active-admin suspension guard. Author review: ZI YANG - validated correcness.
	ErrLastAdmin          = errors.New("cannot suspend the last active admin")
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionCompromised = errors.New("session compromised")
	// AI-generated (edited by ZI YANG): lets the HTTP layer return the recorded 429 without treating lock contention as an authentication failure.
	ErrRefreshInProgress = errors.New("refresh already in progress")
)
