// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for the recorded RS256 JWT and JWKS boundary.
// Author review: PENDING — reviewer to complete

package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/user"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestJWTServiceIssuesAndVerifiesAccessAndRefreshTokens(t *testing.T) {
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	key := testKey(t, "active")
	keys, err := NewKeySet([]Key{key}, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(keys, func() time.Time { return now })
	uid := uuid.New()

	access, err := service.IssueAccessToken(uid, user.AccountRoleAdmin, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifiedAccess, err := service.Verify(access)
	if err != nil {
		t.Fatal(err)
	}
	if verifiedAccess.Subject != uid || verifiedAccess.Role != user.AccountRoleAdmin || verifiedAccess.Type != AccessToken || verifiedAccess.KeyID != key.ID {
		t.Fatalf("verified access token = %#v", verifiedAccess)
	}
	if !verifiedAccess.ExpiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("access expiry = %v, want %v", verifiedAccess.ExpiresAt, now.Add(15*time.Minute))
	}

	refresh, err := service.IssueRefreshToken(uid, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	verifiedRefresh, err := service.Verify(refresh)
	if err != nil {
		t.Fatal(err)
	}
	if verifiedRefresh.Subject != uid || verifiedRefresh.Role != "" || verifiedRefresh.Type != RefreshToken {
		t.Fatalf("verified refresh token = %#v", verifiedRefresh)
	}
	refreshClaims, err := service.VerifyRefresh(context.Background(), refresh)
	if err != nil {
		t.Fatal(err)
	}
	if refreshClaims.UserID != uid || refreshClaims.JTI != verifiedRefresh.JTI || !refreshClaims.ExpiresAt.Equal(verifiedRefresh.ExpiresAt) {
		t.Fatalf("refresh claims = %#v, want mapped verified claims", refreshClaims)
	}
	if _, err := service.VerifyRefresh(context.Background(), access); err == nil {
		t.Fatal("VerifyRefresh() accepted an access token")
	}
}

func TestJWTServiceVerifyRefreshHonorsContextCancellation(t *testing.T) {
	key := testKey(t, "active")
	service := NewService(mustKeySet(t, []Key{key}, key.ID), time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.VerifyRefresh(ctx, "token"); err == nil {
		t.Fatal("VerifyRefresh() succeeded with a cancelled context")
	}
}

func TestJWTServiceVerifyAccessMapsOnlyAccessClaims(t *testing.T) {
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	key := testKey(t, "active")
	service := NewService(mustKeySet(t, []Key{key}, key.ID), func() time.Time { return now })
	access, err := service.IssueAccessToken(uuid.New(), user.AccountRoleStudent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.Verify(access)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.VerifyAccess(context.Background(), access)
	if err != nil {
		t.Fatal(err)
	}
	if claims.JTI != verified.JTI || !claims.ExpiresAt.Equal(verified.ExpiresAt) {
		t.Fatalf("access claims = %#v", claims)
	}

	refresh, err := service.IssueRefreshToken(uuid.New(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyAccess(context.Background(), refresh); err == nil {
		t.Fatal("VerifyAccess() accepted a refresh token")
	}
}

func TestJWTServiceIssuesDomainTokenPairWithRecordedDefaults(t *testing.T) {
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	key := testKey(t, "active")
	service := NewService(mustKeySet(t, []Key{key}, key.ID), func() time.Time { return now })
	account := &user.User{UID: uuid.New(), AccountRole: user.AccountRoleStudent}

	pair, err := service.Issue(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" || pair.RefreshJTI == uuid.Nil {
		t.Fatalf("incomplete token pair = %#v", pair)
	}
	if !pair.RefreshExpiresAt.Equal(now.Add(DefaultRefreshTokenTTL)) {
		t.Fatalf("refresh expiry = %v, want %v", pair.RefreshExpiresAt, now.Add(DefaultRefreshTokenTTL))
	}
	accessClaims, err := service.Verify(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if !accessClaims.ExpiresAt.Equal(now.Add(DefaultAccessTokenTTL)) || accessClaims.Role != user.AccountRoleStudent {
		t.Fatalf("access claims = %#v", accessClaims)
	}
}

func TestJWTServiceRejectsTamperingUnknownKeysAndInvalidTypes(t *testing.T) {
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	active := testKey(t, "active")
	retired := testKey(t, "retired")
	keys, err := NewKeySet([]Key{active, retired}, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(keys, func() time.Time { return now })
	token, err := service.IssueAccessToken(uuid.New(), user.AccountRoleStudent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"tampered signature": tamperSignature(t, token),
		"unknown key":        replaceHeaderKey(t, token, "missing"),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Verify(raw); err == nil {
				t.Fatal("Verify() succeeded for invalid token")
			}
		})
	}

	retiredService := NewService(mustKeySet(t, []Key{retired}, retired.ID), func() time.Time { return now })
	retiredToken, err := retiredService.IssueAccessToken(uuid.New(), user.AccountRoleStudent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(retiredToken); err != nil {
		t.Fatalf("Verify() rejected a retired key: %v", err)
	}

	refresh, err := service.IssueRefreshToken(uuid.New(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(refresh); err != nil {
		t.Fatal(err)
	}
}

func TestJWTServiceAllowsOnlyRecordedClockSkew(t *testing.T) {
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	key := testKey(t, "active")
	service := NewService(mustKeySet(t, []Key{key}, key.ID), func() time.Time { return now })
	token, err := service.IssueAccessToken(uuid.New(), user.AccountRoleStudent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	justExpired := now.Add(time.Minute + ClockSkew)
	expiredService := NewService(mustKeySet(t, []Key{key}, key.ID), func() time.Time { return justExpired })
	if _, err := expiredService.Verify(token); err == nil {
		t.Fatal("Verify() accepted a token beyond the five-second skew")
	}

	withinSkew := now.Add(time.Minute + ClockSkew - time.Nanosecond)
	validService := NewService(mustKeySet(t, []Key{key}, key.ID), func() time.Time { return withinSkew })
	if _, err := validService.Verify(token); err != nil {
		t.Fatalf("Verify() rejected token within clock skew: %v", err)
	}
}

func TestJWTServicePublishesAllPublicKeysAsJWKS(t *testing.T) {
	active := testKey(t, "active")
	retired := testKey(t, "retired")
	service := NewService(mustKeySet(t, []Key{retired, active}, active.ID), time.Now)

	var got struct {
		Keys []jwk `json:"keys"`
	}
	jwksJSON, err := service.JWKS()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jwksJSON, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Keys) != 2 || got.Keys[0].KeyID != "active" || got.Keys[1].KeyID != "retired" {
		t.Fatalf("JWKS keys = %#v", got.Keys)
	}
	for _, publicKey := range got.Keys {
		if publicKey.KeyType != "RSA" || publicKey.Use != "sig" || publicKey.Alg != "RS256" || publicKey.Modulus == "" || publicKey.Exp == "" {
			t.Fatalf("incomplete JWKS key = %#v", publicKey)
		}
		if strings.Contains(publicKey.Modulus, "+") || strings.Contains(publicKey.Modulus, "/") || strings.Contains(publicKey.Modulus, "=") {
			t.Fatalf("JWKS modulus is not base64url: %q", publicKey.Modulus)
		}
		if _, err := base64.RawURLEncoding.DecodeString(publicKey.Modulus); err != nil {
			t.Fatalf("JWKS modulus is invalid: %v", err)
		}
	}
}

func testKey(t *testing.T, id string) Key {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return Key{ID: id, PrivateKey: privateKey, PublicKey: &privateKey.PublicKey}
}

func mustKeySet(t *testing.T, keys []Key, activeID string) KeySet {
	t.Helper()
	keySet, err := NewKeySet(keys, activeID)
	if err != nil {
		t.Fatal(err)
	}
	return keySet
}

func replaceHeaderKey(t *testing.T, raw, keyID string) string {
	t.Helper()
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatal("invalid test token")
	}
	var header map[string]any
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}
	header["kid"] = keyID
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	parts[0] = base64.RawURLEncoding.EncodeToString(encoded)
	return strings.Join(parts, ".")
}

func tamperSignature(t *testing.T, raw string) string {
	t.Helper()
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatal("invalid test token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	signature[0] ^= 1
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	return strings.Join(parts, ".")
}

var _ jwt.Claims = (*claims)(nil)
