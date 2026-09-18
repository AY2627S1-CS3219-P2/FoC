// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added focused tests for bcrypt password hashing and verification.
// Author review: PENDING — reviewer to complete

package user

import "testing"

func TestHashPasswordDoesNotReturnPlaintext(t *testing.T) {
	password := "CorrectHorseBattery9"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == password {
		t.Fatal("HashPassword() returned the plaintext password")
	}
}

func TestCheckPasswordMatchesOnlyTheOriginalPassword(t *testing.T) {
	password := "CorrectHorseBattery9"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !CheckPassword(hash, password) {
		t.Fatal("CheckPassword() rejected the original password")
	}
	if CheckPassword(hash, "WrongPassword9") {
		t.Fatal("CheckPassword() accepted an incorrect password")
	}
}

func TestCheckPasswordRejectsMalformedHash(t *testing.T) {
	if CheckPassword("not-a-bcrypt-hash", "CorrectHorseBattery9") {
		t.Fatal("CheckPassword() accepted a malformed hash")
	}
}
