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
	revocationCheckTimeout        = 500 * time.Millisecond
)

type redisSetter interface {
	// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — atomic cutoff
	// updates preserve the latest timestamp and TTL. Author review: ZI YANG - reviewed and verified.
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

type redisGetter interface {
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
}

// RedisBlocklistWriter persists access-token invalidations in Redis.
type RedisBlocklistWriter struct {
	client redisSetter
}

// NewRedisBlocklistWriter constructs an access-token blocklist adapter.
func NewRedisBlocklistWriter(client redisSetter) *RedisBlocklistWriter {
	return &RedisBlocklistWriter{client: client}
}

// RedisBlocklistReader checks access-token and suspension invalidations.
type RedisBlocklistReader struct {
	client redisGetter
}

// NewRedisBlocklistReader constructs a Redis revocation reader.
func NewRedisBlocklistReader(client redisGetter) *RedisBlocklistReader {
	return &RedisBlocklistReader{client: client}
}

// IsRevoked checks both invalidation keys in one bounded Redis operation.
func (r *RedisBlocklistReader) IsRevoked(ctx context.Context, jti, uid uuid.UUID, issuedAt time.Time) (bool, error) {
	if r.client == nil {
		return false, errors.New("Redis client is required")
	}
	if jti == uuid.Nil || uid == uuid.Nil || issuedAt.IsZero() {
		return false, errors.New("access-token claims are required")
	}
	checkCtx, cancel := context.WithTimeout(ctx, revocationCheckTimeout)
	defer cancel()
	values, err := r.client.MGet(checkCtx,
		accessTokenBlocklistKeyPrefix+jti.String(),
		suspensionKeyPrefix+uid.String(),
	).Result()
	if err != nil {
		return false, fmt.Errorf("read Redis revocation keys: %w", err)
	}
	if len(values) != 2 {
		return false, fmt.Errorf("read Redis revocation keys: got %d values, want 2", len(values))
	}
	if values[0] != nil {
		return true, nil
	}
	if values[1] == nil {
		return false, nil
	}
	suspensionValue, ok := values[1].(string)
	if !ok {
		return true, nil
	}
	suspendedAt, err := time.Parse(time.RFC3339Nano, suspensionValue)
	if err != nil {
		return true, nil
	}
	return !issuedAt.After(suspendedAt), nil
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

// WriteSuspension writes the suspended:uid:<uuid> key holding suspendedAt in
// UTC whole-second RFC3339Nano form, with the given TTL. Readers reject a
// verified token whose iat is at or before this value.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
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
	if err := w.client.Eval(ctx, monotonicCutoffScript, []string{suspensionKeyPrefix + uid.String()}, suspendedAt.UTC().Format(time.RFC3339Nano), ttl.Milliseconds()).Err(); err != nil {
		return fmt.Errorf("set Redis suspension key: %w", err)
	}
	return nil
}

var _ session.BlocklistWriter = (*RedisBlocklistWriter)(nil)

// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — approved monotonic
// cutoff writes shared by role, password and suspension invalidation.
// Author review: ZI YANG - reviewed and verified.
// Normalize optional fractional seconds before comparing UTC timestamp strings.
// Retain the greater TTL so an older in-flight writer cannot undo revocation.
const monotonicCutoffScript = `
local function normalized(value)
 local base, fraction = string.match(value, "^(%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d)%.?(%d*)Z$")
 if not base or #fraction > 9 then return nil end
 return base .. "." .. fraction .. string.rep("0", 9 - #fraction) .. "Z"
end
local incoming = normalized(ARGV[1])
if not incoming then return redis.error_reply("invalid cutoff") end
local existing = redis.call("GET", KEYS[1])
local value = ARGV[1]
local ttl = tonumber(ARGV[2])
local remaining = redis.call("PTTL", KEYS[1])
if existing then
 local current = normalized(existing)
 if not current then return redis.error_reply("invalid existing cutoff") end
 if current > incoming then value = existing end
end
if remaining == -1 then
 redis.call("SET", KEYS[1], value)
else
 redis.call("SET", KEYS[1], value, "PX", math.max(ttl, remaining))
end
return 1
`
