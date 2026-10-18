// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Tests the revocation check against a fake Redis: the two keys
//   user-service writes, its cutoff rule, an unreachable Redis, and the
//   token carried on the context (D-014, D-054, D-056).
// Author review: James Teo — reviewed and verified.

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRedis answers MGet from a map, or with err.
type fakeRedis struct {
	values   map[string]string
	err      error
	keys     []string
	deadline time.Duration
}

func (f *fakeRedis) MGet(ctx context.Context, keys ...string) *redis.SliceCmd {
	f.keys = keys
	if d, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(d)
	}
	out := make([]any, len(keys))
	for i, k := range keys {
		if v, ok := f.values[k]; ok {
			out[i] = v
		}
	}
	return redis.NewSliceResult(out, f.err)
}

func TestRevoked(t *testing.T) {
	user := uuid.New()
	issued := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	c := Claims{UserID: user, Role: "STUDENT", TokenID: "jti-1", IssuedAt: issued}
	suspendedKey := "suspended:uid:" + user.String()

	cases := []struct {
		name   string
		values map[string]string
		want   bool
	}{
		{"neither key", nil, false},
		{"logged out", map[string]string{"jti:jti-1": "1"}, true},
		{"another token logged out", map[string]string{"jti:jti-2": "1"}, false},
		{"suspended after the token was issued", map[string]string{suspendedKey: issued.Add(time.Second).Format(time.RFC3339Nano)}, true},
		{"suspended within the issuing second", map[string]string{suspendedKey: issued.Add(300 * time.Millisecond).Format(time.RFC3339Nano)}, true},
		{"suspended before it (a later login)", map[string]string{suspendedKey: issued.Add(-time.Second).Format(time.RFC3339Nano)}, false},
		// AI-generated (edited by James Teo): user-service writes whole seconds,
		// so a token issued earlier in the suspension's second shows this time.
		{"suspended in the issuing second, as user-service writes it", map[string]string{suspendedKey: issued.Format(time.RFC3339Nano)}, true},
		{"a suspension that cannot be read", map[string]string{suspendedKey: "yesterday"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rdb := &fakeRedis{values: tc.values}
			got, err := NewRevocations(rdb).Revoked(context.Background(), c)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, []string{"jti:jti-1", suspendedKey}, rdb.keys, "user-service's two key formats")
			assert.Positive(t, rdb.deadline)
			assert.LessOrEqual(t, rdb.deadline, revocationTimeout, "a hung Redis delays a request by at most this")
		})
	}
}

func TestRevokedWhenRedisDoesNotAnswer(t *testing.T) {
	down := errors.New("dial tcp: connection refused")
	_, err := NewRevocations(&fakeRedis{err: down}).Revoked(context.Background(), Claims{UserID: uuid.New(), TokenID: "j"})
	require.ErrorIs(t, err, down)
}

func TestTokenOnTheContext(t *testing.T) {
	_, ok := TokenFromContext(context.Background())
	assert.False(t, ok)
	_, ok = TokenFromContext(ContextWithToken(context.Background(), ""))
	assert.False(t, ok, "an empty token is no token")

	got, ok := TokenFromContext(ContextWithToken(context.Background(), "abc"))
	require.True(t, ok)
	assert.Equal(t, "abc", got)
	got, ok = TokenFromContext(context.WithoutCancel(ContextWithToken(context.Background(), "abc")))
	require.True(t, ok, "survives the detached context of a create's retry")
	assert.Equal(t, "abc", got)
}
