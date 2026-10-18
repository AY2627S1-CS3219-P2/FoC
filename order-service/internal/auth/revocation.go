// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: The revocation check against the keys user-service writes to the
//   shared Redis (D-014, D-054), and the bearer token carried on a request's
//   context for forwarding to credit-service (D-056).
// Author review: James Teo — reviewed and verified.

package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// The two key prefixes user-service writes (user-service/internal/repository/
// blocklist_redis.go). user-service is their only writer (D-054).
const (
	blocklistPrefix  = "jti:"
	suspensionPrefix = "suspended:uid:"
)

// revocationTimeout bounds one revocation lookup, so a hung Redis delays a
// request by at most this before the check is skipped (D-056).
const revocationTimeout = 500 * time.Millisecond

// redisGetter is the one go-redis call the check makes; *redis.Client has it.
type redisGetter interface {
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
}

// Revocations reports whether user-service has revoked a verified token.
type Revocations struct {
	rdb redisGetter
}

// NewRevocations returns a Revocations reading rdb.
func NewRevocations(rdb redisGetter) *Revocations {
	return &Revocations{rdb: rdb}
}

// AI-generated (edited by James Teo).
// Revoked reports whether c's token was logged out (its jti is blocklisted) or
// issued at or before the time its account was suspended. user-service writes
// that time in whole seconds, as a token's iat is, so a token issued earlier
// in the suspension's second carries the same time. user-service's refresh can
// reject only an earlier iat because it also revokes the session; there is no
// session here. An error means Redis could not answer.
func (r *Revocations) Revoked(ctx context.Context, c Claims) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, revocationTimeout)
	defer cancel()
	vals, err := r.rdb.MGet(ctx, blocklistPrefix+c.TokenID, suspensionPrefix+c.UserID.String()).Result()
	if err != nil {
		return false, fmt.Errorf("read revocation keys: %w", err)
	}
	if len(vals) != 2 {
		return false, errors.New("read revocation keys: unexpected reply")
	}
	if vals[0] != nil {
		return true, nil
	}
	if vals[1] == nil {
		return false, nil
	}
	raw, ok := vals[1].(string)
	if !ok {
		return false, errors.New("read suspension key: not a string")
	}
	suspendedAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		// Present but unreadable: the account was suspended at some point we
		// cannot place, so the token is treated as revoked.
		return true, nil
	}
	// AI-generated (edited by James Teo).
	return !c.IssuedAt.After(suspendedAt), nil
}

type tokenKey struct{}

// ContextWithToken returns ctx carrying the caller's verified access token, so
// a call made on the caller's behalf can forward it (D-056).
func ContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

// TokenFromContext returns the token ContextWithToken put on ctx, if any.
func TokenFromContext(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenKey{}).(string)
	return t, ok && t != ""
}
