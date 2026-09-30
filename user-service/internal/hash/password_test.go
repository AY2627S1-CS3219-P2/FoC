// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added direct regression coverage for the password-cryptography package.
// Author review: ZI YANG - Repackaged this file and validated tests

package hash

import (
	"strings"
	"testing"
)

func TestHashPasswordDoesNotReturnPlaintext(t *testing.T) {
	password := "CorrectHorseBattery9"
	got, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if got == password {
		t.Fatal("HashPassword() returned plaintext")
	}
}

func TestCheckPasswordMatchesOnlyTheOriginalPassword(t *testing.T) {
	password := "CorrectHorseBattery9"
	passwordHash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !CheckPassword(passwordHash, password) {
		t.Fatal("CheckPassword() rejected the original password")
	}
	if CheckPassword(passwordHash, "WrongPassword9") {
		t.Fatal("CheckPassword() accepted an incorrect password")
	}
}

func TestCheckPasswordRejectsMalformedHash(t *testing.T) {
	if CheckPassword("not-a-bcrypt-hash", "CorrectHorseBattery9") {
		t.Fatal("CheckPassword() accepted a malformed hash")
	}
}

// AI-generated (edited by ZI YANG): the recorded 8–128-character policy must not be limited by bcrypt's 72-byte input cap.
func TestHashPasswordSupportsRecordedLongAndMultibytePasswords(t *testing.T) {
	for name, password := range map[string]string{
		"128 ASCII characters":    strings.Repeat("Abcdef1x", 16),
		"multibyte over 72 bytes": strings.Repeat("Äbcdef1", 18),
	} {
		t.Run(name, func(t *testing.T) {
			passwordHash, err := HashPassword(password)
			if err != nil {
				t.Fatalf("HashPassword() error = %v", err)
			}
			if !CheckPassword(passwordHash, password) {
				t.Fatal("CheckPassword() rejected the original password")
			}
		})
	}
}
