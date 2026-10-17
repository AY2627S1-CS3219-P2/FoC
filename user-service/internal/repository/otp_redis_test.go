// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-09
// Scope: Added Redis adapter tests for the owner-recorded OTP key, TTL, count, lookup, and cleanup behavior.
// Author review: ZI YANG — Validated correctness

package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// AI-generated (edited by James Teo): records every SET, since StoreHash now
// writes the hash and then resets its attempt count.
type fakeRedisSet struct {
	key   string
	value any
	ttl   time.Duration
}

type fakeOTPRedisClient struct {
	sets         []fakeRedisSet // AI-generated (edited by James Teo).
	setErr       error
	incrementKey string
	incremented  int64
	incrementErr error
	expireKey    string
	expireTTL    time.Duration
	expireSet    bool
	expireErr    error
	expireNXKey  string        // AI-generated (edited by James Teo).
	expireNXTTL  time.Duration // AI-generated (edited by James Teo).
	expireNXErr  error         // AI-generated (edited by James Teo).
	getKey       string
	getValue     string
	getErr       error
	deleteKeys   []string
	deleteErr    error
}

func (f *fakeOTPRedisClient) Set(_ context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd {
	f.sets = append(f.sets, fakeRedisSet{key: key, value: value, ttl: ttl}) // AI-generated (edited by James Teo).
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

// AI-generated (edited by James Teo).
func (f *fakeOTPRedisClient) ExpireNX(_ context.Context, key string, ttl time.Duration) *redis.BoolCmd {
	f.expireNXKey, f.expireNXTTL = key, ttl
	return redis.NewBoolResult(true, f.expireNXErr)
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
	// AI-generated (edited by James Teo): the hash is written first, then its
	// attempt count is reset with the same TTL.
	want := []fakeRedisSet{
		{key: "otp:hash:student@u.nus.edu", value: "hashed-otp", ttl: 5 * time.Minute},
		{key: "otp:attempts:student@u.nus.edu", value: 0, ttl: 5 * time.Minute},
	}
	if !reflect.DeepEqual(client.sets, want) {
		t.Fatalf("SET calls = %#v, want %#v", client.sets, want)
	}
}

// AI-generated (edited by James Teo): an attempt is counted with INCR, keeping
// the TTL StoreHash set; EXPIRE NX only covers a count INCR had to create.
func TestRedisOTPStoreIncrementsAttemptCountKeepingCodeTTL(t *testing.T) {
	client := &fakeOTPRedisClient{incremented: 3}
	count, err := NewRedisOTPStore(client).IncrementAttemptCount(context.Background(), "student@u.nus.edu")
	if err != nil || count != 3 {
		t.Fatalf("IncrementAttemptCount() = %d, %v; want 3, nil", count, err)
	}
	if client.incrementKey != "otp:attempts:student@u.nus.edu" || client.expireNXKey != client.incrementKey || client.expireNXTTL != 5*time.Minute {
		t.Fatalf("INCR/EXPIRE NX = %q / %q with TTL %v", client.incrementKey, client.expireNXKey, client.expireNXTTL)
	}
	if client.expireKey != "" {
		t.Fatalf("plain EXPIRE on %q would extend the count past its code", client.expireKey)
	}
}

// AI-generated (edited by James Teo).
func TestRedisOTPStoreReportsAttemptCountTTLFailure(t *testing.T) {
	redisFailure := errors.New("Redis unavailable")
	client := &fakeOTPRedisClient{incremented: 1, expireNXErr: redisFailure}
	if _, err := NewRedisOTPStore(client).IncrementAttemptCount(context.Background(), "student@u.nus.edu"); !errors.Is(err, redisFailure) {
		t.Fatalf("error = %v, want Redis failure", err)
	}
}

// AI-generated (edited by James Teo): discarding a code keeps the request count,
// so wrong guesses cannot reset the resend limit.
func TestRedisOTPStoreDeleteHashKeepsRequestCount(t *testing.T) {
	client := &fakeOTPRedisClient{}
	if err := NewRedisOTPStore(client).DeleteHash(context.Background(), "student@u.nus.edu"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"otp:hash:student@u.nus.edu", "otp:attempts:student@u.nus.edu"}; !reflect.DeepEqual(client.deleteKeys, want) {
		t.Fatalf("DEL keys = %#v, want %#v", client.deleteKeys, want)
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

// AI-generated (edited by James Teo): Delete also removes the attempt count.
func TestRedisOTPStoreDeletesAllOTPKeys(t *testing.T) {
	client := &fakeOTPRedisClient{}
	if err := NewRedisOTPStore(client).Delete(context.Background(), "student@u.nus.edu"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"otp:hash:student@u.nus.edu", "otp:count:student@u.nus.edu", "otp:attempts:student@u.nus.edu"}; !reflect.DeepEqual(client.deleteKeys, want) {
		t.Fatalf("DEL keys = %#v, want %#v", client.deleteKeys, want)
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
		// AI-generated (edited by James Teo).
		"increment attempt count": func(store *RedisOTPStore) error {
			_, err := store.IncrementAttemptCount(context.Background(), "student@u.nus.edu")
			return err
		},
		"delete hash": func(store *RedisOTPStore) error {
			return store.DeleteHash(context.Background(), "student@u.nus.edu")
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
