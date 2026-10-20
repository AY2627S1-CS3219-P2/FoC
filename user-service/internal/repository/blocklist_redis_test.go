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

type fakeRedisGetter struct {
	keys   []string
	values []interface{}
	err    error
}

func (f *fakeRedisGetter) MGet(_ context.Context, keys ...string) *redis.SliceCmd {
	f.keys = append([]string(nil), keys...)
	return redis.NewSliceResult(f.values, f.err)
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

func TestRedisBlocklistReaderChecksJTIAndSuspensionKeys(t *testing.T) {
	uid, jti := uuid.New(), uuid.New()
	suspendedAt := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		values   []interface{}
		issuedAt time.Time
		want     bool
	}{
		{name: "no keys", values: []interface{}{nil, nil}, issuedAt: suspendedAt.Add(time.Second)},
		{name: "JTI key", values: []interface{}{"1", nil}, issuedAt: suspendedAt.Add(time.Second), want: true},
		{name: "before suspension", values: []interface{}{nil, suspendedAt.Format(time.RFC3339Nano)}, issuedAt: suspendedAt.Add(-time.Second), want: true},
		{name: "at suspension", values: []interface{}{nil, suspendedAt.Format(time.RFC3339Nano)}, issuedAt: suspendedAt, want: true},
		{name: "after suspension", values: []interface{}{nil, suspendedAt.Format(time.RFC3339Nano)}, issuedAt: suspendedAt.Add(time.Second)},
		{name: "unparseable suspension", values: []interface{}{nil, "not-a-time"}, issuedAt: suspendedAt.Add(time.Second), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeRedisGetter{values: tt.values}
			reader := NewRedisBlocklistReader(client)
			got, err := reader.IsRevoked(context.Background(), jti, uid, tt.issuedAt)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("IsRevoked() = %t, want %t", got, tt.want)
			}
			wantKeys := []string{"jti:" + jti.String(), "suspended:uid:" + uid.String()}
			if len(client.keys) != len(wantKeys) || client.keys[0] != wantKeys[0] || client.keys[1] != wantKeys[1] {
				t.Fatalf("MGET keys = %#v, want %#v", client.keys, wantKeys)
			}
		})
	}
}

func TestRedisBlocklistReaderFailsOnRedisAndMalformedReplies(t *testing.T) {
	reader := NewRedisBlocklistReader(&fakeRedisGetter{err: errors.New("Redis unavailable")})
	if _, err := reader.IsRevoked(context.Background(), uuid.New(), uuid.New(), time.Now()); err == nil {
		t.Fatal("Redis error was accepted")
	}
	reader = NewRedisBlocklistReader(&fakeRedisGetter{values: []interface{}{nil}})
	if _, err := reader.IsRevoked(context.Background(), uuid.New(), uuid.New(), time.Now()); err == nil {
		t.Fatal("wrong-length Redis reply was accepted")
	}
}
