// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused startup configuration tests for JWT token lifetimes.
// Author review: Validated tests reflects intended behvaiour

package main

import (
	"testing"
	"time"
)

func TestParseTokenTTL(t *testing.T) {
	tests := map[string]struct {
		value        string
		defaultValue time.Duration
		want         time.Duration
		wantError    bool
	}{
		"uses recorded default when unset": {defaultValue: 15 * time.Minute, want: 15 * time.Minute},
		"parses configured duration":       {value: "168h", defaultValue: time.Minute, want: 168 * time.Hour},
		"rejects malformed duration":       {value: "seven days", defaultValue: time.Minute, wantError: true},
		"rejects non-positive duration":    {value: "0s", defaultValue: time.Minute, wantError: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseTokenTTL(tt.value, tt.defaultValue)
			if (err != nil) != tt.wantError {
				t.Fatalf("parseTokenTTL() error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil && got != tt.want {
				t.Fatalf("parseTokenTTL() = %v, want %v", got, tt.want)
			}
		})
	}
}
