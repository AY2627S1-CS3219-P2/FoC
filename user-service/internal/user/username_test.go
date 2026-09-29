// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Added table-driven tests for the recorded username policy.
// Author review: PENDING — reviewer to complete

package user

import (
	"strings"
	"testing"
)

func TestValidateUsername(t *testing.T) {
	maxLengthUsername := strings.Repeat("a", 128)
	tests := map[string]struct {
		username string
		wantErr  bool
	}{
		"accepts letters and digits": {username: "student123"},
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — covers the
		// recorded ASCII-only username policy. Author review: PENDING.
		"rejects Unicode letters":    {username: "学生123", wantErr: true},
		"rejects Cyrillic lookalike": {username: "аdmin", wantErr: true},
		"rejects full-width letters": {username: "ａｄｍｉｎ", wantErr: true},
		"accepts 128 characters":     {username: maxLengthUsername},
		"rejects empty username":     {username: "", wantErr: true},
		"rejects more than 128":      {username: maxLengthUsername + "a", wantErr: true},
		"rejects spaces":             {username: "student user", wantErr: true},
		"rejects punctuation":        {username: "student_user", wantErr: true},
		"rejects symbols":            {username: "student@nus", wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUsername(%q) error = %v, want error: %t", tt.username, err, tt.wantErr)
			}
			if tt.wantErr && err != ErrInvalidUsername {
				t.Fatalf("ValidateUsername(%q) error = %v, want ErrInvalidUsername", tt.username, err)
			}
		})
	}
}
