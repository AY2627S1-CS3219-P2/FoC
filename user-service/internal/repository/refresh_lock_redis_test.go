// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-28
// Scope: Added Redis refresh-lock adapter regression tests from the recorded refresh-lock behavior.
// Author review: ZI YANG - validated correctness

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeRedisSetNexter struct {
	key   string
	value any
	ttl   time.Duration
	set   bool
	err   error
}

func (f *fakeRedisSetNexter) SetNX(_ context.Context, key string, value any, ttl time.Duration) *redis.BoolCmd {
	f.key = key
	f.value = value
	f.ttl = ttl
	return redis.NewBoolResult(f.set, f.err)
}

func TestRedisRefreshLockUsesRecordedKeyAndTTL(t *testing.T) {
	client := &fakeRedisSetNexter{set: true}
	locked, err := NewRedisRefreshLock(client).Acquire(context.Background(), "hashed-token", 5*time.Second)
	if err != nil || !locked {
		t.Fatalf("Acquire() = %v, %v; want true, nil", locked, err)
	}
	if client.key != "refresh_lock:hashed-token" || client.value != "1" || client.ttl != 5*time.Second {
		t.Fatalf("SetNX() = key %q, value %#v, ttl %v", client.key, client.value, client.ttl)
	}
}

func TestRedisRefreshLockReportsContentionAndRedisFailure(t *testing.T) {
	redisFailure := errors.New("Redis unavailable")
	tests := map[string]struct {
		client     *fakeRedisSetNexter
		wantLocked bool
		wantErr    error
	}{
		"existing lock": {client: &fakeRedisSetNexter{}, wantLocked: false},
		"Redis failure": {client: &fakeRedisSetNexter{err: redisFailure}, wantErr: redisFailure},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			locked, err := NewRedisRefreshLock(tt.client).Acquire(context.Background(), "hashed-token", 5*time.Second)
			if locked != tt.wantLocked || !errors.Is(err, tt.wantErr) {
				t.Fatalf("Acquire() = %v, %v; want %v, %v", locked, err, tt.wantLocked, tt.wantErr)
			}
		})
	}
}
