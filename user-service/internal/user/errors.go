// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added repository errors that callers must distinguish.
// Author review: COMPLETE BY ZI YANG

package user

import "errors"

var (
	ErrNotFound          = errors.New("user not found")
	ErrDuplicateEmail    = errors.New("email address is already in use")
	ErrDuplicateUsername = errors.New("username is already in use")
)
