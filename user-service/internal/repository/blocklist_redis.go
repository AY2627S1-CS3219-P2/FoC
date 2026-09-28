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

const (
	accessTokenBlocklistKeyPrefix = "jti:"
	suspensionKeyPrefix           = "suspended:uid:"
)

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

// AI-generated (edited by ZI YANG): assigns the lifetime calculation to the
// caller, which passes the token's remaining lifetime into this adapter.
// BlockAccessToken writes the jti:<uuid> key with the given TTL. The caller
// passes the token's remaining lifetime.
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

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — updated this comment
// to remove a stale cross-service reader assertion. Author review: ZI YANG.

// WriteSuspension writes the suspended:uid:<uuid> key holding suspendedAt, with
// the given TTL.
func (w *RedisBlocklistWriter) WriteSuspension(ctx context.Context, uid uuid.UUID, suspendedAt time.Time, ttl time.Duration) error {
	if w.client == nil {
		return errors.New("Redis client is required")
	}
	if uid == uuid.Nil {
		return errors.New("suspended account ID is required")
	}
	if suspendedAt.IsZero() {
		return errors.New("suspension timestamp is required")
	}
	if ttl <= 0 {
		return errors.New("suspension key TTL must be positive")
	}
	if err := w.client.Set(ctx, suspensionKeyPrefix+uid.String(), suspendedAt.UTC().Format(time.RFC3339Nano), ttl).Err(); err != nil {
		return fmt.Errorf("set Redis suspension key: %w", err)
	}
	return nil
}

var _ session.BlocklistWriter = (*RedisBlocklistWriter)(nil)
