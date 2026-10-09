// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-09
// Scope: Added the Redis adapter for owner-recorded OTP hashes, request counts, TTLs, lookup, and cleanup.
// Author review: ZI YANG - validated correctness

package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	otpHashKeyPrefix  = "otp:hash:"
	otpCountKeyPrefix = "otp:count:"
	// OTPHashTTL is the recorded lifetime for a stored OTP hash.
	OTPHashTTL = 5 * time.Minute
	// OTPCountTTL is the recorded rate-limit window for OTP requests.
	OTPCountTTL = 10 * time.Minute
)

type otpRedisClient interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Incr(ctx context.Context, key string) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// RedisOTPStore persists OTP state and request limits in Redis.
type RedisOTPStore struct {
	client otpRedisClient
}

// NewRedisOTPStore constructs an OTP Redis adapter.
func NewRedisOTPStore(client otpRedisClient) *RedisOTPStore {
	return &RedisOTPStore{client: client}
}

// StoreHash writes an OTP hash with the recorded five-minute TTL.
func (s *RedisOTPStore) StoreHash(ctx context.Context, email, hash string) error {
	if s.client == nil {
		return errors.New("Redis client is required")
	}
	if email == "" {
		return errors.New("OTP email is required")
	}
	if hash == "" {
		return errors.New("OTP hash is required")
	}
	if err := s.client.Set(ctx, otpHashKey(email), hash, OTPHashTTL).Err(); err != nil {
		return fmt.Errorf("store OTP hash: %w", err)
	}
	return nil
}

// IncrementRequestCount increments the request count and renews its recorded
// ten-minute TTL.
func (s *RedisOTPStore) IncrementRequestCount(ctx context.Context, email string) (int64, error) {
	if s.client == nil {
		return 0, errors.New("Redis client is required")
	}
	if email == "" {
		return 0, errors.New("OTP email is required")
	}
	key := otpCountKey(email)
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("increment OTP request count: %w", err)
	}
	expires, err := s.client.Expire(ctx, key, OTPCountTTL).Result()
	if err != nil {
		return 0, fmt.Errorf("set OTP request-count TTL: %w", err)
	}
	if !expires {
		return 0, errors.New("OTP request-count key was not found while setting TTL")
	}
	return count, nil
}

// LookupHash retrieves an OTP hash. A missing key is not a Redis failure.
func (s *RedisOTPStore) LookupHash(ctx context.Context, email string) (string, bool, error) {
	if s.client == nil {
		return "", false, errors.New("Redis client is required")
	}
	if email == "" {
		return "", false, errors.New("OTP email is required")
	}
	hash, err := s.client.Get(ctx, otpHashKey(email)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get OTP hash: %w", err)
	}
	return hash, true, nil
}

// RequestCount retrieves an existing OTP request count. A missing key has no
// count and is not a Redis failure.
func (s *RedisOTPStore) RequestCount(ctx context.Context, email string) (int64, bool, error) {
	if s.client == nil {
		return 0, false, errors.New("Redis client is required")
	}
	if email == "" {
		return 0, false, errors.New("OTP email is required")
	}
	value, err := s.client.Get(ctx, otpCountKey(email)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get OTP request count: %w", err)
	}
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse OTP request count: %w", err)
	}
	return count, true, nil
}

// Delete removes both the OTP hash and request-count keys for an email.
func (s *RedisOTPStore) Delete(ctx context.Context, email string) error {
	if s.client == nil {
		return errors.New("Redis client is required")
	}
	if email == "" {
		return errors.New("OTP email is required")
	}
	if err := s.client.Del(ctx, otpHashKey(email), otpCountKey(email)).Err(); err != nil {
		return fmt.Errorf("delete OTP keys: %w", err)
	}
	return nil
}

func otpHashKey(email string) string { return otpHashKeyPrefix + email }

func otpCountKey(email string) string { return otpCountKeyPrefix + email }
