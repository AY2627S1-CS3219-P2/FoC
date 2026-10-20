// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: The revocation check against the keys user-service writes to the shared
//   Redis (D-014, D-054). Copied from order-service/internal/auth rather than
//   imported (root AGENTS.md §4 rule 2).
// Author review: PENDING — <reviewer to complete>

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
// request by at most this before the write is refused (D-041).
const revocationTimeout = 500 * time.Millisecond

// ErrRedisUnreachable wraps a lookup that never reached Redis: a dial or
// network failure, a timeout, or a closed client. Any other error from Revoked
// means Redis answered but the keys could not be read, such as an error reply
// (NOAUTH, WRONGPASS, CROSSSLOT, MOVED, READONLY). Admin writes are refused in
// both cases (D-041); the two are kept apart so the log says which it was.
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

// Revoked reports whether c's token was logged out (its jti is blocklisted) or
// issued at or before the time its account was suspended. user-service writes
// that time in whole seconds, as a token's iat is, so a token issued earlier
// in the suspension's second carries the same time. user-service's refresh can
// reject only an earlier iat because it also revokes the session; there is no
// session here.
//
// An error wrapping ErrRedisUnreachable means Redis could not be reached; any
// other error means it answered, but not with the keys.
func (r *Revocations) Revoked(ctx context.Context, c Claims) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, revocationTimeout)
	defer cancel()
	vals, err := r.rdb.MGet(ctx, blocklistPrefix+c.TokenID, suspensionPrefix+c.UserID.String()).Result()
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
	return !c.IssuedAt.After(suspendedAt), nil
}

// unreachable reports whether err means the lookup never reached Redis, the
// case that is logged as an outage. An error reply is Redis answering, so it is
// never one of these, whatever caused it.
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
