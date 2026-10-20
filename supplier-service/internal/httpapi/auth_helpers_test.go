// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Test helpers for the token check: a test RSA key served as user-service's
//   key set from an httptest server, tokens signed the way user-service signs
//   them, and a fake revocation checker.
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/auth"
)

const testKid = "test-key"

var (
	keyOnce sync.Once
	testKey *rsa.PrivateKey
)

// signingKey generates one RSA key for the whole package.
func signingKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if testKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return testKey
}

// startKeySet serves the test key as user-service's JWKS and returns a
// verifier pointed at it. status other than 200 makes every fetch fail.
func startKeySet(t *testing.T, status int) *auth.Verifier {
	t.Helper()
	pub := &signingKey(t).PublicKey
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": testKid,
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	return auth.NewVerifier(srv.URL, srv.Client())
}

// tokenSpec is the claims of a token; the zero value is a valid ADMIN access
// token that expires in 15 minutes. edit changes the claims before signing.
type tokenSpec struct {
	sub  uuid.UUID
	jti  string
	role string
	edit func(jwt.MapClaims)
}

// mintToken signs an access token the way user-service does (RS256, a kid,
// iss, aud, typ, role, jti, iat, exp).
func mintToken(t *testing.T, spec tokenSpec) string {
	t.Helper()
	if spec.sub == uuid.Nil {
		spec.sub = uuid.New()
	}
	if spec.jti == "" {
		spec.jti = uuid.NewString()
	}
	if spec.role == "" {
		spec.role = "ADMIN"
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": spec.sub.String(), "role": spec.role, "typ": "access", "jti": spec.jti,
		"iss": "campusrun-user-service", "aud": []string{"campusrun-api"},
		"iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(),
	}
	if spec.edit != nil {
		spec.edit(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = testKid
	signed, err := tok.SignedString(signingKey(t))
	require.NoError(t, err)
	return signed
}

// fakeRevocations stands in for the Redis lookup. revokedJTIs are logged-out
// tokens; err, when set, is what every lookup returns.
type fakeRevocations struct {
	revokedJTIs map[string]bool
	err         error
}

func (f *fakeRevocations) Revoked(_ context.Context, c auth.Claims) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.revokedJTIs[c.TokenID], nil
}
