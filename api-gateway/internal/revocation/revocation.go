// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-20
// Scope: The revocation check against the keys user-service writes to the
//   shared Redis (D-054), copied from order-service's internal/auth rather
//   than imported (root AGENTS.md §4 rule 2), and the Redis client it reads
//   through. Any failure to read the keys is an error the caller refuses on.
// Author review: James Teo — reviewed and verified.

// Package revocation reports whether user-service has revoked an access token
// the gateway has already verified: logged out (its jti is blocklisted), or
// issued at or before its account was suspended. It only reads the two keys
// user-service writes (D-054); nothing here writes to Redis.
package revocation

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

// lookupTimeout bounds one lookup, so a hung Redis holds a request for at most
// this before it is refused.
const lookupTimeout = 500 * time.Millisecond

// redisGetter is the one go-redis call the check makes; *redis.Client has it.
type redisGetter interface {
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
}

// Checker reads user-service's revocation keys. It is safe for concurrent use.
type Checker struct {
	rdb redisGetter
}

// New returns a Checker reading rdb, normally the client NewRedisClient builds.
func New(rdb redisGetter) *Checker {
	return &Checker{rdb: rdb}
}

// Revoked reports whether the token tokenID, issued at issuedAt to the user
// subject, was logged out or issued at or before that user's suspension.
// user-service writes the suspension time in whole seconds, as a token's iat
// is, so a token issued earlier in the suspension's second carries the same
// time and is refused too.
//
// Any error means the keys could not be read: Redis did not answer within
// lookupTimeout, answered with an error, or sent a reply of the wrong shape.
// The caller must refuse the request rather than let it through (D-077).
func (c *Checker) Revoked(ctx context.Context, tokenID, subject string, issuedAt time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	vals, err := c.rdb.MGet(ctx, blocklistPrefix+tokenID, suspensionPrefix+subject).Result()
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
	return !issuedAt.After(suspendedAt), nil
}

// NewRedisClient returns a client for the Redis at redisURL, for New. It does
// not connect: the gateway starts without Redis and the check fails until
// Redis answers.
//
// go-redis ignores a context's deadline on socket reads and writes unless
// ContextTimeoutEnabled is set, so without it a Redis that accepts the
// connection and then stops answering would hold a request for the default
// multi-second socket timeout instead of lookupTimeout.
//
// The error is fixed text: the URL can carry a password, and go-redis's own
// error would quote it.
func NewRedisClient(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, errors.New("REDIS_URL is not a valid Redis URL")
	}
	opts.ContextTimeoutEnabled = true
	return redis.NewClient(opts), nil
}
