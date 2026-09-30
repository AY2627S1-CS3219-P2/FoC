// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded Redis access-token blocklist adapter.
// Author review: Validated tests reflects intended behvaiour

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type fakeRedisSetter struct {
	key   string
	value any
	ttl   time.Duration
	err   error
}

func (f *fakeRedisSetter) Set(_ context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd {
	f.key = key
	f.value = value
	f.ttl = ttl
	return redis.NewStatusResult("OK", f.err)
}

func TestRedisBlocklistWriterStoresRecordedJTIKeyAndTTL(t *testing.T) {
	client := &fakeRedisSetter{}
	writer := NewRedisBlocklistWriter(client)
	jti := uuid.New()
	ttl := 14*time.Minute + 30*time.Second

	if err := writer.BlockAccessToken(context.Background(), jti, ttl); err != nil {
		t.Fatal(err)
	}
	if client.key != "jti:"+jti.String() || client.value != "1" || client.ttl != ttl {
		t.Fatalf("Redis set = key=%q value=%#v ttl=%v", client.key, client.value, client.ttl)
	}
}

func TestRedisBlocklistWriterPreservesSetErrors(t *testing.T) {
	setFailure := errors.New("Redis unavailable")
	writer := NewRedisBlocklistWriter(&fakeRedisSetter{err: setFailure})

	if err := writer.BlockAccessToken(context.Background(), uuid.New(), time.Minute); !errors.Is(err, setFailure) {
		t.Fatalf("error = %v, want Redis failure", err)
	}
}

func TestRedisBlocklistWriterRejectsInvalidInputs(t *testing.T) {
	writer := NewRedisBlocklistWriter(&fakeRedisSetter{})
	if err := writer.BlockAccessToken(context.Background(), uuid.Nil, time.Minute); err == nil {
		t.Fatal("nil JTI was accepted")
	}
	if err := writer.BlockAccessToken(context.Background(), uuid.New(), 0); err == nil {
		t.Fatal("zero TTL was accepted")
	}
}
