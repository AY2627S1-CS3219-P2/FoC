package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAuthRedis struct {
	redis.UniversalClient
	getFn func(ctx context.Context, key string) *redis.StringCmd
}

func (f *fakeAuthRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if f.getFn != nil {
		return f.getFn(ctx, key)
	}
	return redis.NewStringResult("", redis.Nil)
}

func TestVerifier_IsUserSuspended(t *testing.T) {
	userID := uuid.New()
	targetKey := suspensionPrefix + userID.String()

	t.Run("nil redis returns false without error", func(t *testing.T) {
		v := NewVerifier("http://unused", nil)
		suspended, err := v.IsUserSuspended(context.Background(), userID, time.Now())
		require.NoError(t, err)
		assert.False(t, suspended)
	})

	t.Run("key not found in redis means not suspended", func(t *testing.T) {
		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				assert.Equal(t, targetKey, key)
				return redis.NewStringResult("", redis.Nil)
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, time.Now())
		require.NoError(t, err)
		assert.False(t, suspended)
	})

	t.Run("redis error propagates with ErrRedisUnreachable", func(t *testing.T) {
		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				return redis.NewStringResult("", errors.New("connection reset by peer"))
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, time.Now())
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrRedisUnreachable))
		assert.False(t, suspended)
	})

	t.Run("unreadable suspension timestamp fails closed", func(t *testing.T) {
		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				return redis.NewStringResult("not-a-timestamp", nil)
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, time.Now())
		require.NoError(t, err)
		assert.True(t, suspended)
	})

	t.Run("zero authenticatedAt timestamp fails closed when key exists", func(t *testing.T) {
		suspendedAt := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				return redis.NewStringResult(suspendedAt, nil)
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, time.Time{})
		require.NoError(t, err)
		assert.True(t, suspended)
	})

	t.Run("connection established before suspension is treated as suspended", func(t *testing.T) {
		t0 := time.Now().UTC()
		authenticatedAt := t0.Add(-2 * time.Minute)
		suspendedAt := t0.Add(-1 * time.Minute)

		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				return redis.NewStringResult(suspendedAt.Format(time.RFC3339Nano), nil)
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, authenticatedAt)
		require.NoError(t, err)
		assert.True(t, suspended, "authenticatedAt before suspendedAt should be treated as suspended")
	})

	t.Run("connection established at the same moment as suspension is treated as suspended", func(t *testing.T) {
		t0 := time.Now().UTC().Truncate(time.Second)
		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				return redis.NewStringResult(t0.Format(time.RFC3339Nano), nil)
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, t0)
		require.NoError(t, err)
		assert.True(t, suspended, "authenticatedAt equal to suspendedAt should be treated as suspended")
	})

	t.Run("connection established after suspension (reactivated user) is not suspended", func(t *testing.T) {
		t0 := time.Now().UTC()
		suspendedAt := t0.Add(-5 * time.Minute)
		authenticatedAt := t0.Add(-1 * time.Minute)

		rdb := &fakeAuthRedis{
			getFn: func(ctx context.Context, key string) *redis.StringCmd {
				return redis.NewStringResult(suspendedAt.Format(time.RFC3339Nano), nil)
			},
		}
		v := NewVerifier("http://unused", rdb)
		suspended, err := v.IsUserSuspended(context.Background(), userID, authenticatedAt)
		require.NoError(t, err)
		assert.False(t, suspended, "authenticatedAt after suspendedAt must not be treated as suspended")
	})
}
