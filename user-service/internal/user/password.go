// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added the recorded Unicode-aware password policy validator.
// Author review: Validated correctness

package user

import (
	"unicode"
	"unicode/utf8"
)

// ValidatePassword enforces the password policy for new or changed passwords.
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < 8 || length > 128 {
		return ErrInvalidPassword
	}

	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		hasUpper = hasUpper || unicode.IsUpper(r)
		hasLower = hasLower || unicode.IsLower(r)
		hasDigit = hasDigit || unicode.IsDigit(r)
	}
	if !hasUpper || !hasLower || !hasDigit {
		return ErrInvalidPassword
	}
	return nil
}
