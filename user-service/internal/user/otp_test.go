// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-09
// Scope: Added focused tests for the owner-recorded OTP generator and hash representation.
// Author review: ZI YANG — Validated correctness

package user

import (
	"regexp"
	"testing"
)

func TestGenerateOTPReturnsSixDigitNumericCode(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9]{6}$`)
	for range 100 {
		otp, err := GenerateOTP()
		if err != nil {
			t.Fatalf("generate OTP: %v", err)
		}
		if !pattern.MatchString(otp) {
			t.Fatalf("OTP = %q, want exactly six digits", otp)
		}
	}
}

func TestHashOTPIsStableAndDoesNotReturnPlaintext(t *testing.T) {
	const otp = "012345"
	hash := HashOTP(otp)
	if hash == otp {
		t.Fatal("OTP hash must not equal the plaintext OTP")
	}
	if hash != HashOTP(otp) {
		t.Fatal("same OTP produced different hash values")
	}
	if hash == HashOTP("012346") {
		t.Fatal("different OTPs produced the same hash value")
	}
}

func TestOTPRequestLimitExceeded(t *testing.T) {
	tests := map[string]struct {
		count int64
		want  bool
	}{
		"at limit":    {count: OTPRequestLimit, want: false},
		"over limit":  {count: OTPRequestLimit + 1, want: true},
		"below limit": {count: 0, want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := OTPRequestLimitExceeded(tt.count); got != tt.want {
				t.Fatalf("OTPRequestLimitExceeded(%d) = %t, want %t", tt.count, got, tt.want)
			}
		})
	}

}

// AI-generated (edited by James Teo): the fifth attempt is still compared; the
// sixth is over the limit.
func TestOTPAttemptLimitExceeded(t *testing.T) {
	tests := map[string]struct {
		count int64
		want  bool
	}{
		"first attempt": {count: 1, want: false},
		"at limit":      {count: OTPAttemptLimit, want: false},
		"over limit":    {count: OTPAttemptLimit + 1, want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := OTPAttemptLimitExceeded(tt.count); got != tt.want {
				t.Fatalf("OTPAttemptLimitExceeded(%d) = %t, want %t", tt.count, got, tt.want)
			}
		})
	}
}
