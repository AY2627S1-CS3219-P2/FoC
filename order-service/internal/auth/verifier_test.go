// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Tests the access-token verifier against an httptest key set with real
//   RS256 tokens: the claims it returns, every refusal, key-fetch throttling
//   and an unreachable key set (D-023, D-056).
// Author review: James Teo — reviewed and verified.

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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	keyOnce sync.Once
	keyA    *rsa.PrivateKey
	keyB    *rsa.PrivateKey
)

// testKeys generates two RSA keys once for the whole package.
func testKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if keyA, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if keyB, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return keyA, keyB
}

// keySet serves a JWKS with the given keys and counts the fetches; status
// other than 200 makes every fetch fail.
type keySet struct {
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	status  int
	fetches atomic.Int32
}

func (k *keySet) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	k.fetches.Add(1)
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.status != 0 && k.status != http.StatusOK {
		w.WriteHeader(k.status)
		return
	}
	var out struct {
		Keys []map[string]string `json:"keys"`
	}
	for kid, pub := range k.keys {
		out.Keys = append(out.Keys, map[string]string{
			"kty": "RSA", "kid": kid,
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func newKeySet(t *testing.T, keys map[string]*rsa.PublicKey) (*keySet, *Verifier) {
	t.Helper()
	ks := &keySet{keys: keys}
	srv := httptest.NewServer(ks)
	t.Cleanup(srv.Close)
	return ks, NewVerifier(srv.URL, srv.Client())
}

// sign makes a token with claims, signed by key under kid with method.
func sign(t *testing.T, method jwt.SigningMethod, key any, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

// AI-generated (edited by James Teo).
// accessClaims are the claims user-service puts on an access token, its iss
// and aud included (user-service/internal/jwt/jwt.go).
func accessClaims(sub uuid.UUID, now time.Time) jwt.MapClaims {
	return jwt.MapClaims{
		"sub": sub.String(), "role": "STUDENT", "typ": "access", "jti": uuid.NewString(),
		"iss": "campusrun-user-service", "aud": []string{"campusrun-api"},
		"iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
	}
}

func TestVerifyReturnsTheClaims(t *testing.T) {
	a, _ := testKeys(t)
	_, v := newKeySet(t, map[string]*rsa.PublicKey{"k1": &a.PublicKey})
	sub, now := uuid.New(), time.Now().Truncate(time.Second)
	claims := accessClaims(sub, now)

	got, err := v.Verify(context.Background(), sign(t, jwt.SigningMethodRS256, a, "k1", claims))

	require.NoError(t, err)
	assert.Equal(t, sub, got.UserID)
	assert.Equal(t, "STUDENT", got.Role)
	assert.Equal(t, claims["jti"], got.TokenID)
	assert.Equal(t, now.Unix(), got.IssuedAt.Unix())
}

func TestVerifyRefusals(t *testing.T) {
	a, b := testKeys(t)
	_, v := newKeySet(t, map[string]*rsa.PublicKey{"k1": &a.PublicKey})
	now := time.Now()
	with := func(edit func(jwt.MapClaims)) jwt.MapClaims {
		c := accessClaims(uuid.New(), now)
		edit(c)
		return c
	}
	pubDER := a.PublicKey.N.Bytes()

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"empty", "", ErrMalformed},
		{"not a JWT", "not-a-token", ErrMalformed},
		{"expired", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() })), ErrExpired},
		{"no exp", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { delete(c, "exp") })), ErrMalformed},
		{"no iat", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { delete(c, "iat") })), ErrMalformed},
		{"no jti", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { delete(c, "jti") })), ErrMalformed},
		{"sub not a UUID", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { c["sub"] = "student2" })), ErrMalformed},
		{"a refresh token", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { c["typ"] = "refresh" })), ErrWrongType},
		// AI-generated (edited by James Teo): iss and aud exactly as user-service issues them.
		{"another issuer", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { c["iss"] = "campusrun-other-service" })), ErrInvalidSignature},
		{"no issuer", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { delete(c, "iss") })), ErrMalformed},
		{"another audience", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { c["aud"] = []string{"campusrun-internal"} })), ErrInvalidSignature},
		{"the audience as a plain string", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { c["aud"] = "campusrun-other" })), ErrInvalidSignature},
		{"no audience", sign(t, jwt.SigningMethodRS256, a, "k1", with(func(c jwt.MapClaims) { delete(c, "aud") })), ErrMalformed},
		{"no kid", sign(t, jwt.SigningMethodRS256, a, "", accessClaims(uuid.New(), now)), ErrMalformed},
		{"signed by another key", sign(t, jwt.SigningMethodRS256, b, "k1", accessClaims(uuid.New(), now)), ErrInvalidSignature},
		{"HS256 with the public key as the secret", sign(t, jwt.SigningMethodHS256, pubDER, "k1", accessClaims(uuid.New(), now)), ErrInvalidSignature},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tc.token)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestAnUnknownKidRefetchesAtMostOncePerInterval(t *testing.T) {
	// A rotation is picked up by one refetch; an invented kid cannot make
	// order-service hammer user-service (minRefreshInterval).
	a, b := testKeys(t)
	ks, v := newKeySet(t, map[string]*rsa.PublicKey{"k1": &a.PublicKey})
	ctx := context.Background()

	_, err := v.Verify(ctx, sign(t, jwt.SigningMethodRS256, a, "k1", accessClaims(uuid.New(), time.Now())))
	require.NoError(t, err)
	require.EqualValues(t, 1, ks.fetches.Load())

	ks.mu.Lock()
	ks.keys["k2"] = &b.PublicKey
	ks.mu.Unlock()
	for range 3 {
		_, err = v.Verify(ctx, sign(t, jwt.SigningMethodRS256, b, "k2", accessClaims(uuid.New(), time.Now())))
		require.ErrorIs(t, err, ErrInvalidSignature, "within the interval the new kid is not fetched")
	}
	assert.EqualValues(t, 1, ks.fetches.Load())

	v.mu.Lock()
	v.lastAttempt = time.Now().Add(-minRefreshInterval)
	v.mu.Unlock()
	_, err = v.Verify(ctx, sign(t, jwt.SigningMethodRS256, b, "k2", accessClaims(uuid.New(), time.Now())))
	require.NoError(t, err, "after the interval the rotation is fetched")
	assert.EqualValues(t, 2, ks.fetches.Load())
}

func TestUnreachableKeysAreReportedAsSuch(t *testing.T) {
	a, _ := testKeys(t)
	ks, v := newKeySet(t, map[string]*rsa.PublicKey{"k1": &a.PublicKey})
	ks.status = http.StatusBadGateway

	_, err := v.Verify(context.Background(), sign(t, jwt.SigningMethodRS256, a, "k1", accessClaims(uuid.New(), time.Now())))

	require.ErrorIs(t, err, ErrKeysUnavailable)
	assert.False(t, errors.Is(err, ErrInvalidSignature))
}

func TestACachedKeyOutlivesAnOutage(t *testing.T) {
	// Once fetched, a key keeps verifying even if user-service goes down.
	a, _ := testKeys(t)
	ks, v := newKeySet(t, map[string]*rsa.PublicKey{"k1": &a.PublicKey})
	ctx := context.Background()
	_, err := v.Verify(ctx, sign(t, jwt.SigningMethodRS256, a, "k1", accessClaims(uuid.New(), time.Now())))
	require.NoError(t, err)

	ks.mu.Lock()
	ks.status = http.StatusServiceUnavailable
	ks.mu.Unlock()
	_, err = v.Verify(ctx, sign(t, jwt.SigningMethodRS256, a, "k1", accessClaims(uuid.New(), time.Now())))

	require.NoError(t, err)
	assert.EqualValues(t, 1, ks.fetches.Load())
}
