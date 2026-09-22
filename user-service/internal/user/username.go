// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Added the recorded Unicode-aware alphanumeric username validator.
// Author review: PENDING — reviewer to complete

package user

import (
	"unicode"
	"unicode/utf8"
)

// ValidateUsername enforces the username length and alphanumeric policy.
func ValidateUsername(username string) error {
	length := utf8.RuneCountInString(username)
	if length == 0 || length > 128 {
		return ErrInvalidUsername
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ErrInvalidUsername
		}
	}
	return nil
}
