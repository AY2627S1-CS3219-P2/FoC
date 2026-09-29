// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Added the recorded Unicode-aware alphanumeric username validator.
// Author review: PENDING — reviewer to complete

package user

import (
	"unicode/utf8"
)

// ValidateUsername enforces the username length and alphanumeric policy.
func ValidateUsername(username string) error {
	length := utf8.RuneCountInString(username)
	if length == 0 || length > 128 {
		return ErrInvalidUsername
	}
	for _, r := range username {
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — applies the
		// recorded ASCII-only username policy. Author review: PENDING.
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ErrInvalidUsername
		}
	}
	return nil
}
