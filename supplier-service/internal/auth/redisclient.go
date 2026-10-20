// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: The Redis client the revocation check reads through, built with
//   context deadlines enabled so Revoked's 500 ms bound holds on the socket.
// Author review: PENDING — <reviewer to complete>

package auth

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

// NewRedisClient returns a client for the Redis at redisURL, for NewRevocations.
// go-redis ignores a context's deadline on socket reads and writes unless
// ContextTimeoutEnabled is set, so without it a Redis that accepts the
// connection and then stops answering would hold an admin write for the
// default multi-second socket timeout instead of revocationTimeout.
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
