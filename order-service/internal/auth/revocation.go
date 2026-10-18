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
	"io"
	"net"
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

// AI-generated (edited by James Teo).
// ErrRedisUnreachable wraps a lookup that never reached Redis: a dial or
// network failure, a timeout, or a closed client. Only this skips the
// revocation check (D-056). Any other error from Revoked means Redis answered
// but the keys could not be read, such as an error reply (NOAUTH, WRONGPASS,
// CROSSSLOT, MOVED, READONLY), and the request must be refused.
var ErrRedisUnreachable = errors.New("auth: Redis unreachable")

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
// session here.
//
// AI-generated (edited by James Teo).
// An error wrapping ErrRedisUnreachable means Redis could not be reached; any
// other error means it answered, but not with the keys.
func (r *Revocations) Revoked(ctx context.Context, c Claims) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, revocationTimeout)
	defer cancel()
	vals, err := r.rdb.MGet(ctx, blocklistPrefix+c.TokenID, suspensionPrefix+c.UserID.String()).Result()
	// AI-generated (edited by James Teo).
	if err != nil {
		if unreachable(err) {
			return false, fmt.Errorf("read revocation keys: %w: %w", ErrRedisUnreachable, err)
		}
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

// AI-generated (edited by James Teo).
// unreachable reports whether err means the lookup never reached Redis, the
// one case D-056 lets the check be skipped. An error reply is Redis answering,
// so it is never one of these, whatever caused it.
func unreachable(err error) bool {
	var reply redis.Error
	if errors.As(err, &reply) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, redis.ErrClosed) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF)
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
