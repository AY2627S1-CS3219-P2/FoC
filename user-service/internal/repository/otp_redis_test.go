// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-09
// Scope: Added Redis adapter tests for the owner-recorded OTP key, TTL, count, lookup, and cleanup behavior.
// Author review: ZI YANG — Validated correctness

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeOTPRedisClient struct {
	setKey       string
	setValue     any
	setTTL       time.Duration
	setErr       error
	incrementKey string
	incremented  int64
	incrementErr error
	expireKey    string
	expireTTL    time.Duration
	expireSet    bool
	expireErr    error
	getKey       string
	getValue     string
	getErr       error
	deleteKeys   []string
	deleteErr    error
}

func (f *fakeOTPRedisClient) Set(_ context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd {
	f.setKey, f.setValue, f.setTTL = key, value, ttl
	return redis.NewStatusResult("OK", f.setErr)
}

func (f *fakeOTPRedisClient) Incr(_ context.Context, key string) *redis.IntCmd {
	f.incrementKey = key
	return redis.NewIntResult(f.incremented, f.incrementErr)
}

func (f *fakeOTPRedisClient) Expire(_ context.Context, key string, ttl time.Duration) *redis.BoolCmd {
	f.expireKey, f.expireTTL = key, ttl
	return redis.NewBoolResult(f.expireSet, f.expireErr)
}

func (f *fakeOTPRedisClient) Get(_ context.Context, key string) *redis.StringCmd {
	f.getKey = key
	return redis.NewStringResult(f.getValue, f.getErr)
}

func (f *fakeOTPRedisClient) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.deleteKeys = keys
	return redis.NewIntResult(int64(len(keys)), f.deleteErr)
}

func TestRedisOTPStoreUsesRecordedHashKeyAndTTL(t *testing.T) {
	client := &fakeOTPRedisClient{}
	if err := NewRedisOTPStore(client).StoreHash(context.Background(), "student@u.nus.edu", "hashed-otp"); err != nil {
		t.Fatal(err)
	}
	if client.setKey != "otp:hash:student@u.nus.edu" || client.setValue != "hashed-otp" || client.setTTL != 5*time.Minute {
		t.Fatalf("Set() = key %q, value %#v, TTL %v", client.setKey, client.setValue, client.setTTL)
	}
}

func TestRedisOTPStoreIncrementsCountAndRefreshesRecordedTTL(t *testing.T) {
	client := &fakeOTPRedisClient{incremented: 2, expireSet: true}
	count, err := NewRedisOTPStore(client).IncrementRequestCount(context.Background(), "student@u.nus.edu")
	if err != nil || count != 2 {
		t.Fatalf("IncrementRequestCount() = %d, %v; want 2, nil", count, err)
	}
	if client.incrementKey != "otp:count:student@u.nus.edu" || client.expireKey != client.incrementKey || client.expireTTL != 10*time.Minute {
		t.Fatalf("INCR/EXPIRE = %q / %q with TTL %v", client.incrementKey, client.expireKey, client.expireTTL)
	}
}

func TestRedisOTPStoreReturnsMissingHashWithoutRedisFailure(t *testing.T) {
	client := &fakeOTPRedisClient{getErr: redis.Nil}
	hash, found, err := NewRedisOTPStore(client).LookupHash(context.Background(), "student@u.nus.edu")
	if err != nil || found || hash != "" {
		t.Fatalf("LookupHash() = %q, %t, %v; want empty, false, nil", hash, found, err)
	}
}

func TestRedisOTPStoreReadsExistingRequestCount(t *testing.T) {
	client := &fakeOTPRedisClient{getValue: "4"}
	count, found, err := NewRedisOTPStore(client).RequestCount(context.Background(), "student@u.nus.edu")
	if err != nil || !found || count != 4 {
		t.Fatalf("RequestCount() = %d, %t, %v; want 4, true, nil", count, found, err)
	}
	if client.getKey != "otp:count:student@u.nus.edu" {
		t.Fatalf("GET key = %q", client.getKey)
	}
}

func TestRedisOTPStoreDeletesBothOTPKeys(t *testing.T) {
	client := &fakeOTPRedisClient{}
	if err := NewRedisOTPStore(client).Delete(context.Background(), "student@u.nus.edu"); err != nil {
		t.Fatal(err)
	}
	if len(client.deleteKeys) != 2 || client.deleteKeys[0] != "otp:hash:student@u.nus.edu" || client.deleteKeys[1] != "otp:count:student@u.nus.edu" {
		t.Fatalf("DEL keys = %#v", client.deleteKeys)
	}
}

func TestRedisOTPStorePreservesRedisFailures(t *testing.T) {
	redisFailure := errors.New("Redis unavailable")
	tests := map[string]func(*RedisOTPStore) error{
		"store hash": func(store *RedisOTPStore) error {
			return store.StoreHash(context.Background(), "student@u.nus.edu", "hash")
		},
		"increment count": func(store *RedisOTPStore) error {
			_, err := store.IncrementRequestCount(context.Background(), "student@u.nus.edu")
			return err
		},
		"delete keys": func(store *RedisOTPStore) error {
			return store.Delete(context.Background(), "student@u.nus.edu")
		},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeOTPRedisClient{setErr: redisFailure, incrementErr: redisFailure, deleteErr: redisFailure}
			if err := call(NewRedisOTPStore(client)); !errors.Is(err, redisFailure) {
				t.Fatalf("error = %v, want Redis failure", err)
			}
		})
	}
}

func TestRedisOTPStoreReportsRequestCountTTLFailure(t *testing.T) {
	redisFailure := errors.New("Redis unavailable")
	client := &fakeOTPRedisClient{incremented: 1, expireErr: redisFailure}
	if _, err := NewRedisOTPStore(client).IncrementRequestCount(context.Background(), "student@u.nus.edu"); !errors.Is(err, redisFailure) {
		t.Fatalf("error = %v, want Redis failure", err)
	}
}
