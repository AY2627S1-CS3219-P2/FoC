// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-10-28
// Scope: Characterizes existing key-cache behavior after JWKS key removal;
//   records an observed coverage gap without choosing a new refresh policy.
// Author review: PENDING

package auth

import (
	"context"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLegacyAuditRemovedKeyRemainsCachedWithoutUnknownKid(t *testing.T) {
	a, b := testKeys(t)
	ks, v := newKeySet(t, map[string]*rsa.PublicKey{"old": &a.PublicKey})
	token := sign(t, jwt.SigningMethodRS256, a, "old", accessClaims(uuid.New(), time.Now()))
	_, err := v.Verify(context.Background(), token)
	require.NoError(t, err)
	ks.mu.Lock()
	ks.keys = map[string]*rsa.PublicKey{"new": &b.PublicKey}
	ks.mu.Unlock()
	// Passing the fetch throttle does not refresh a known key. This is a
	// characterization of current behavior, not approval of that policy.
	v.mu.Lock()
	v.lastAttempt = time.Now().Add(-minRefreshInterval - time.Second)
	v.mu.Unlock()
	_, err = v.Verify(context.Background(), token)
	require.NoError(t, err, "current behavior still accepts a token signed by the removed key")
	require.EqualValues(t, 1, ks.fetches.Load(), "the changed JWKS was not fetched")
}
