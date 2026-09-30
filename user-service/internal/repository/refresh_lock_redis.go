// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-28
// Scope: Implemented the recorded five-second Redis debounce lock for refresh-token rotation.
// Author review: ZI YANG - validated correctness

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const refreshLockKeyPrefix = "refresh_lock:"

type redisSetNexter interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
}

// RedisRefreshLock acquires short-lived refresh-token debounce locks in Redis.
type RedisRefreshLock struct {
	client redisSetNexter
}

// NewRedisRefreshLock constructs a Redis-backed refresh lock.
func NewRedisRefreshLock(client redisSetNexter) *RedisRefreshLock {
	return &RedisRefreshLock{client: client}
}

// Acquire creates the lock for tokenHash when no concurrent refresh owns it.
func (l *RedisRefreshLock) Acquire(ctx context.Context, tokenHash string, ttl time.Duration) (bool, error) {
	if l.client == nil {
		return false, errors.New("Redis client is required")
	}
	if tokenHash == "" {
		return false, errors.New("refresh token hash is required")
	}
	if ttl <= 0 {
		return false, errors.New("refresh lock TTL must be positive")
	}
	locked, err := l.client.SetNX(ctx, refreshLockKeyPrefix+tokenHash, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("set Redis refresh lock: %w", err)
	}
	return locked, nil
}
