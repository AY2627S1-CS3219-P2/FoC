// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-09
// Scope: Added the owner-recorded cryptographic six-digit OTP generator, SHA-256 hashing, and request-limit predicate.
// Author review: ZI YANG — Validated correctness

package user

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

const (
	otpCodeRange = 1_000_000
	// OTPRequestLimit is the maximum number of OTP requests allowed in the
	// recorded ten-minute Redis window.
	OTPRequestLimit = 3
	// AI-generated (edited by James Teo): the wrong-guess limit for one stored code.
	// OTPAttemptLimit is the number of verification attempts one stored OTP
	// accepts; the attempt after it discards the code.
	OTPAttemptLimit = 5
)

// GenerateOTP returns a cryptographically secure six-digit numeric OTP.
func GenerateOTP() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(otpCodeRange))
	if err != nil {
		return "", fmt.Errorf("generate OTP: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

// HashOTP returns the SHA-256 hash representation stored in Redis.
func HashOTP(otp string) string {
	sum := sha256.Sum256([]byte(otp))
	return hex.EncodeToString(sum[:])
}

// OTPRequestLimitExceeded reports whether an OTP request count exceeds the
// recorded limit.
func OTPRequestLimitExceeded(count int64) bool {
	return count > OTPRequestLimit
}

// AI-generated (edited by James Teo): mirrors OTPRequestLimitExceeded for the
// wrong-guess limit.
// OTPAttemptLimitExceeded reports whether a verification attempt count exceeds
// the per-code limit.
func OTPAttemptLimitExceeded(count int64) bool {
	return count > OTPAttemptLimit
}
