// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-20
// Scope: Unit tests for Revoked against a fake MGET: each key, the suspension
//   boundary, and every way the keys can fail to be read.
// Author review: James Teo — reviewed and verified.

package revocation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeRedis answers MGet with a fixed reply and records what it was asked.
type fakeRedis struct {
	vals []any
	err  error

	keys        []string
	hadDeadline time.Duration
}

func (f *fakeRedis) MGet(ctx context.Context, keys ...string) *redis.SliceCmd {
	f.keys = keys
	if deadline, ok := ctx.Deadline(); ok {
		f.hadDeadline = time.Until(deadline)
	}
	return redis.NewSliceResult(f.vals, f.err)
}

const (
	tokenID = "4b2d5c1e-7f3a-4e8b-9c6d-0a1b2c3d4e5f"
	subject = "9f8e7d6c-5b4a-4321-8fed-cba987654321"
)

func TestRevoked(t *testing.T) {
	suspendedAt := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	stamp := suspendedAt.Format(time.RFC3339)

	tests := []struct {
		name     string
		vals     []any
		issuedAt time.Time
		want     bool
	}{
		{name: "neither key: not revoked", vals: []any{nil, nil}, issuedAt: suspendedAt, want: false},
		{name: "jti blocklisted: revoked", vals: []any{"1", nil}, issuedAt: suspendedAt, want: true},
		{name: "issued a second before the suspension: revoked", vals: []any{nil, stamp}, issuedAt: suspendedAt.Add(-time.Second), want: true},
		{name: "issued in the suspension's second: revoked", vals: []any{nil, stamp}, issuedAt: suspendedAt, want: true},
		{name: "issued a second after the suspension: not revoked", vals: []any{nil, stamp}, issuedAt: suspendedAt.Add(time.Second), want: false},
		{name: "unreadable suspension value: revoked", vals: []any{nil, "not a time"}, issuedAt: suspendedAt.Add(time.Hour), want: true},
		{name: "both keys: revoked", vals: []any{"1", stamp}, issuedAt: suspendedAt.Add(time.Hour), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdb := &fakeRedis{vals: tt.vals}

			got, err := New(rdb).Revoked(context.Background(), tokenID, subject, tt.issuedAt)

			if err != nil {
				t.Fatalf("Revoked: %v", err)
			}
			if got != tt.want {
				t.Errorf("Revoked = %v, want %v", got, tt.want)
			}
			wantKeys := []string{"jti:" + tokenID, "suspended:uid:" + subject}
			if strings.Join(rdb.keys, " ") != strings.Join(wantKeys, " ") {
				t.Errorf("MGET keys = %v, want %v", rdb.keys, wantKeys)
			}
		})
	}
}

// Every way the keys can fail to be read is an error, never a "not revoked":
// the gateway refuses the request on any of them.
func TestRevokedReportsUnreadableKeysAsErrors(t *testing.T) {
	tests := []struct {
		name string
		vals []any
		err  error
	}{
		{name: "Redis errors", err: errors.New("dial tcp: connection refused")},
		{name: "Redis times out", err: context.DeadlineExceeded},
		{name: "a closed client", err: redis.ErrClosed},
		{name: "an error reply", err: errors.New("NOAUTH Authentication required.")},
		{name: "a reply of the wrong length", vals: []any{nil}},
		{name: "a non-string suspension value", vals: []any{nil, int64(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			revoked, err := New(&fakeRedis{vals: tt.vals, err: tt.err}).Revoked(context.Background(), tokenID, subject, time.Now())

			if err == nil {
				t.Fatalf("Revoked = %v with no error, want an error", revoked)
			}
			if revoked {
				t.Error("Revoked = true alongside an error; the caller must see only the error")
			}
		})
	}
}

func TestRevokedBoundsTheLookup(t *testing.T) {
	rdb := &fakeRedis{vals: []any{nil, nil}}

	if _, err := New(rdb).Revoked(context.Background(), tokenID, subject, time.Now()); err != nil {
		t.Fatalf("Revoked: %v", err)
	}
	if rdb.hadDeadline <= 0 || rdb.hadDeadline > lookupTimeout {
		t.Errorf("MGET deadline = %v from now, want at most %v", rdb.hadDeadline, lookupTimeout)
	}
}

func TestNewRedisClient(t *testing.T) {
	client, err := NewRedisClient("redis://redis:6379/0")
	if err != nil {
		t.Fatalf("NewRedisClient: %v", err)
	}
	defer func() { _ = client.Close() }()
	if !client.Options().ContextTimeoutEnabled {
		t.Error("ContextTimeoutEnabled = false, so a hung Redis would outlast lookupTimeout")
	}
}

func TestNewRedisClientErrorDoesNotLeakThePassword(t *testing.T) {
	_, err := NewRedisClient("redis://user:s3cret-pass@redis:not-a-port")
	if err == nil {
		t.Fatal("NewRedisClient: expected an error, got nil")
	}
	if strings.Contains(err.Error(), "s3cret-pass") {
		t.Errorf("error %q contains the password", err)
	}
}
