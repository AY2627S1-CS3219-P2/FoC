// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Redis access-token and user-suspension revocation checks (D-014, D-054).
//   Adapted from order-service/internal/auth per D-041/D-056.
// Author review: PENDING - reviewer to complete

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

const (
	blocklistPrefix  = "jti:"
	suspensionPrefix = "suspended:uid:"
)

const revocationTimeout = 500 * time.Millisecond

// ErrRedisUnreachable wraps a lookup that never reached Redis. Only this skips the revocation check (D-056).
var ErrRedisUnreachable = errors.New("auth: Redis unreachable")

type redisGetter interface {
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
}

// Revocations reports whether user-service has revoked a verified token.
type Revocations struct {
	rdb redisGetter
}

// NewRevocations returns a Revocations instance querying rdb.
func NewRevocations(rdb redisGetter) *Revocations {
	return &Revocations{rdb: rdb}
}

// Revoked reports whether token was blocklisted on logout or issued before an account suspension.
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
		return true, nil
	}
	return !c.IssuedAt.After(suspendedAt), nil
}

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
