// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added table-driven tests for the recorded password policy.
// Author review: PENDING — reviewer to complete

package user

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	maxLengthPassword := strings.Repeat("a", 126) + "A1"
	tests := map[string]struct {
		password string
		wantErr  bool
	}{
		"accepts valid password":     {password: "ValidPass1"},
		"accepts eight characters":   {password: "Abcdef1x"},
		"accepts unicode characters": {password: "Äbcdef1x"},
		"accepts 128 characters":     {password: maxLengthPassword},
		"rejects fewer than eight":   {password: "Abc1xyz", wantErr: true},
		"rejects more than 128":      {password: maxLengthPassword + "b", wantErr: true},
		"rejects missing uppercase":  {password: "validpass1", wantErr: true},
		"rejects missing lowercase":  {password: "VALIDPASS1", wantErr: true},
		"rejects missing digit":      {password: "ValidPassword", wantErr: true},
		"rejects empty password":     {password: "", wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword(%q) error = %v, want error: %t", tt.password, err, tt.wantErr)
			}
			if tt.wantErr && err != ErrInvalidPassword {
				t.Fatalf("ValidatePassword(%q) error = %v, want ErrInvalidPassword", tt.password, err)
			}
		})
	}
}
