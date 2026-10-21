// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Tests the revocation check with a fake Redis: neither key, logged out,
//   suspended before, at and after the token's iat, an unreadable suspension,
//   Redis errors and the exact key names (D-014, D-054). Copied from
//   order-service/internal/auth.
// Author review: PENDING — <reviewer to complete>

package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
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
		// user-service writes whole seconds,
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

// replyError stands in for an error reply from Redis: go-redis's own type is
// internal, and redis.Error is matched by its RedisError method.
type replyError string

func (e replyError) Error() string { return string(e) }

func (replyError) RedisError() {}

func TestRevokedWhenRedisDoesNotAnswer(t *testing.T) {
	// Both a lookup that never reached Redis and one Redis answered with an
	// error (a missing password, a cluster's redirect) leave the keys unread, so
	// both are errors; only the first is reported as ErrRedisUnreachable.
	cases := []struct {
		name        string
		err         error
		unreachable bool
	}{
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"no such host", &net.DNSError{Err: "no such host", Name: "redis", IsNotFound: true}, true},
		{"the lookup's deadline", context.DeadlineExceeded, true},
		{"a closed client", redis.ErrClosed, true},
		{"the connection dropped mid-reply", io.ErrUnexpectedEOF, true},
		{"NOAUTH", replyError("NOAUTH Authentication required."), false},
		{"WRONGPASS", replyError("WRONGPASS invalid username-password pair or user is disabled."), false},
		{"CROSSSLOT", replyError("CROSSSLOT Keys in request don't hash to the same slot"), false},
		{"MOVED", replyError("MOVED 3999 127.0.0.1:6381"), false},
		{"READONLY", replyError("READONLY You can't write against a read only replica."), false},
		{"a wrapped error reply", fmt.Errorf("%w", replyError("LOADING Redis is loading the dataset in memory")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRevocations(&fakeRedis{err: tc.err}).Revoked(context.Background(), Claims{UserID: uuid.New(), TokenID: "j"})
			require.ErrorIs(t, err, tc.err, "the cause is kept for the log")
			assert.Equal(t, tc.unreachable, errors.Is(err, ErrRedisUnreachable))
		})
	}
}
