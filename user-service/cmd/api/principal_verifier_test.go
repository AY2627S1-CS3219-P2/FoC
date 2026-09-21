// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the concrete JWT-to-HTTP-principal adapter.
// Author review: PENDING — reviewer to complete

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"foc/user-service/internal/jwt"
	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

func TestJWTPrincipalVerifierAcceptsOnlyAccessTokens(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keySet, err := jwt.NewKeySet([]jwt.Key{{ID: "active", PrivateKey: privateKey, PublicKey: &privateKey.PublicKey}}, "active")
	if err != nil {
		t.Fatal(err)
	}
	service := jwt.NewService(keySet, time.Now)
	verifier := jwtPrincipalVerifier{service: service}
	uid := uuid.New()
	accessToken, err := service.IssueAccessToken(uid, user.AccountRoleAdmin, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), accessToken)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != uid || principal.Role != user.AccountRoleAdmin {
		t.Fatalf("principal = %#v", principal)
	}
	refreshToken, err := service.IssueRefreshToken(uid, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), refreshToken); err == nil {
		t.Fatal("Verify() accepted a refresh token")
	}
}
