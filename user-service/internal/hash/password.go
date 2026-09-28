// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added bcrypt password hashing and verification helpers.
// Author review: Repackaged this file and validated correctness

// Package hash contains user-service password cryptography.
package hash

import (
	"crypto/sha256"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword returns a bcrypt hash for a plaintext password.
func HashPassword(password string) (string, error) {
	// AI-generated (edited by ZI YANG): preserve the recorded 128-character policy within bcrypt's 72-byte cap.
	digest := sha256.Sum256([]byte(password))
	hash, err := bcrypt.GenerateFromPassword(digest[:], bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword reports whether password matches a bcrypt hash.
func CheckPassword(hash, password string) bool {
	digest := sha256.Sum256([]byte(password))
	return bcrypt.CompareHashAndPassword([]byte(hash), digest[:]) == nil
}
