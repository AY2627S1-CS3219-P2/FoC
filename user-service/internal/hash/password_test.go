// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added direct regression coverage for the password-cryptography package.
// Author review: Repackaged this file and validated tests

package hash

import "testing"

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
