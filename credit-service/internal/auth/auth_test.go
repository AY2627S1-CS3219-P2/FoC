package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func startJWKSServer(t *testing.T, key *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pub := key.Public().(*rsa.PublicKey)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": kid,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	}))
}

func mintToken(t *testing.T, key *rsa.PrivateKey, kid string, sub string, role, typ string, exp, iat time.Time, jti string, iss, aud string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":  sub,
		"role": role,
		"typ":  typ,
		"exp":  exp.Unix(),
		"iat":  iat.Unix(),
		"jti":  jti,
		"iss":  iss,
		"aud":  aud,
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestVerifier_ValidToken(t *testing.T) {
	key := generateTestKey(t)
	kid := "key-1"
	srv := startJWKSServer(t, key, kid)
	defer srv.Close()

	verifier := NewVerifier(srv.URL, srv.Client())
	userID := uuid.New()
	now := time.Now().Truncate(time.Second)

	raw := mintToken(t, key, kid, userID.String(), "STUDENT", "access", now.Add(1*time.Hour), now, "jti-123", issuer, audience)
	claims, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("expected valid token, got err: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("expected UserID %s, got %s", userID, claims.UserID)
	}
	if claims.Role != "STUDENT" {
		t.Errorf("expected Role STUDENT, got %s", claims.Role)
	}
	if claims.TokenID != "jti-123" {
		t.Errorf("expected TokenID jti-123, got %s", claims.TokenID)
	}
}

func TestVerifier_Errors(t *testing.T) {
	key := generateTestKey(t)
	kid := "key-1"
	srv := startJWKSServer(t, key, kid)
	defer srv.Close()

	verifier := NewVerifier(srv.URL, srv.Client())
	userID := uuid.New()
	now := time.Now().Truncate(time.Second)

	// Malformed
	if _, err := verifier.Verify(context.Background(), "invalid-token"); !errors.Is(err, ErrMalformed) {
		t.Errorf("expected ErrMalformed, got: %v", err)
	}

	// Expired
	expToken := mintToken(t, key, kid, userID.String(), "STUDENT", "access", now.Add(-1*time.Hour), now.Add(-2*time.Hour), "jti-1", issuer, audience)
	if _, err := verifier.Verify(context.Background(), expToken); !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got: %v", err)
	}

	// Wrong Typ
	refresh := mintToken(t, key, kid, userID.String(), "STUDENT", "refresh", now.Add(1*time.Hour), now, "jti-1", issuer, audience)
	if _, err := verifier.Verify(context.Background(), refresh); !errors.Is(err, ErrWrongType) {
		t.Errorf("expected ErrWrongType, got: %v", err)
	}

	// Invalid Issuer
	badIss := mintToken(t, key, kid, userID.String(), "STUDENT", "access", now.Add(1*time.Hour), now, "jti-1", "wrong-issuer", audience)
	if _, err := verifier.Verify(context.Background(), badIss); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

type fakeRedis struct {
	mgetFn func(ctx context.Context, keys ...string) *redis.SliceCmd
}

func (f *fakeRedis) MGet(ctx context.Context, keys ...string) *redis.SliceCmd {
	return f.mgetFn(ctx, keys...)
}

func TestRevocations(t *testing.T) {
	userID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	claims := Claims{
		UserID:   userID,
		Role:     "STUDENT",
		TokenID:  "token-abc",
		IssuedAt: now,
	}

	// 1. Not revoked
	rdbClean := &fakeRedis{
		mgetFn: func(ctx context.Context, keys ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{nil, nil})
			return cmd
		},
	}
	revClean := NewRevocations(rdbClean)
	revoked, err := revClean.Revoked(context.Background(), claims)
	if err != nil || revoked {
		t.Fatalf("expected not revoked, got revoked=%v, err=%v", revoked, err)
	}

	// 2. Blocklisted JTI
	rdbBlock := &fakeRedis{
		mgetFn: func(ctx context.Context, keys ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{"1", nil})
			return cmd
		},
	}
	revBlock := NewRevocations(rdbBlock)
	revoked, err = revBlock.Revoked(context.Background(), claims)
	if err != nil || !revoked {
		t.Fatalf("expected revoked for blocklist, got revoked=%v, err=%v", revoked, err)
	}

	// 3. Suspended account before token was issued
	rdbSuspendedOld := &fakeRedis{
		mgetFn: func(ctx context.Context, keys ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{nil, now.Add(-10 * time.Minute).Format(time.RFC3339Nano)})
			return cmd
		},
	}
	revSuspOld := NewRevocations(rdbSuspendedOld)
	revoked, err = revSuspOld.Revoked(context.Background(), claims)
	if err != nil || revoked {
		t.Fatalf("expected token issued after suspension to not be revoked, got %v", revoked)
	}

	// 4. Suspended account after token was issued (token issued <= suspension time)
	rdbSuspendedNew := &fakeRedis{
		mgetFn: func(ctx context.Context, keys ...string) *redis.SliceCmd {
			cmd := redis.NewSliceCmd(ctx)
			cmd.SetVal([]any{nil, now.Add(10 * time.Minute).Format(time.RFC3339Nano)})
			return cmd
		},
	}
	revSuspNew := NewRevocations(rdbSuspendedNew)
	revoked, err = revSuspNew.Revoked(context.Background(), claims)
	if err != nil || !revoked {
		t.Fatalf("expected token issued before suspension to be revoked, got %v", revoked)
	}
}
