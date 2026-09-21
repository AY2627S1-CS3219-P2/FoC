// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Implemented the recorded Redis access-token blocklist adapter.
// Author review: COMPLETED BY ZI YANG

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/user-service/internal/session"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const accessTokenBlocklistKeyPrefix = "jti:"

type redisSetter interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

// RedisBlocklistWriter persists access-token invalidations in Redis.
type RedisBlocklistWriter struct {
	client redisSetter
}

// NewRedisBlocklistWriter constructs an access-token blocklist adapter.
func NewRedisBlocklistWriter(client redisSetter) *RedisBlocklistWriter {
	return &RedisBlocklistWriter{client: client}
}

// BlockAccessToken stores the recorded JTI key for exactly the remaining
// access-token lifetime.
func (w *RedisBlocklistWriter) BlockAccessToken(ctx context.Context, jti uuid.UUID, ttl time.Duration) error {
	if w.client == nil {
		return errors.New("Redis client is required")
	}
	if jti == uuid.Nil {
		return errors.New("access-token JTI is required")
	}
	if ttl <= 0 {
		return errors.New("access-token blocklist TTL must be positive")
	}
	if err := w.client.Set(ctx, accessTokenBlocklistKeyPrefix+jti.String(), "1", ttl).Err(); err != nil {
		return fmt.Errorf("set Redis access-token blocklist key: %w", err)
	}
	return nil
}

var _ session.BlocklistWriter = (*RedisBlocklistWriter)(nil)
