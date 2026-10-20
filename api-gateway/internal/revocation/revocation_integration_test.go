// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-20
// Scope: Integration test for Revoked against a real Redis in testcontainers,
//   with the keys written exactly as user-service writes them, then with Redis
//   stopped.
// Author review: James Teo — reviewed and verified.

//go:build integration

package revocation

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRevokedOverRealRedis(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start Redis: %v", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}

	rdb, err := NewRedisClient("redis://" + net.JoinHostPort(host, port.Port()))
	if err != nil {
		t.Fatalf("NewRedisClient: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	checker := New(rdb)

	// The keys as user-service writes them (internal/repository/
	// blocklist_redis.go): "1" under jti:<jti> on logout, and the suspension
	// time in RFC 3339 whole seconds under suspended:uid:<uuid>.
	suspendedAt := time.Now().UTC().Truncate(time.Second)
	loggedOut, suspended, unreadable, untouched := newID(), newID(), newID(), newID()
	for key, value := range map[string]string{
		"jti:" + loggedOut:            "1",
		"suspended:uid:" + suspended:  suspendedAt.Format(time.RFC3339),
		"suspended:uid:" + unreadable: "garbled",
	} {
		if err := rdb.Set(ctx, key, value, time.Minute).Err(); err != nil {
			t.Fatalf("SET %s: %v", key, err)
		}
	}

	tests := []struct {
		name             string
		tokenID, subject string
		issuedAt         time.Time
		want             bool
	}{
		{"neither key: not revoked", newID(), untouched, suspendedAt, false},
		{"jti blocklisted: revoked", loggedOut, untouched, suspendedAt, true},
		{"issued a second before the suspension: revoked", newID(), suspended, suspendedAt.Add(-time.Second), true},
		{"issued in the suspension's second: revoked", newID(), suspended, suspendedAt, true},
		{"issued a second after the suspension: not revoked", newID(), suspended, suspendedAt.Add(time.Second), false},
		{"unreadable suspension value: revoked", newID(), unreadable, suspendedAt.Add(time.Hour), true},
		{"both keys: revoked", loggedOut, suspended, suspendedAt.Add(time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checker.Revoked(ctx, tt.tokenID, tt.subject, tt.issuedAt)
			if err != nil {
				t.Fatalf("Revoked: %v", err)
			}
			if got != tt.want {
				t.Errorf("Revoked = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("with Redis stopped, the check errors within the timeout", func(t *testing.T) {
		timeout := 5 * time.Second
		if err := container.Stop(ctx, &timeout); err != nil {
			t.Fatalf("stop Redis: %v", err)
		}

		start := time.Now()
		revoked, err := checker.Revoked(ctx, newID(), untouched, suspendedAt)
		if err == nil {
			t.Fatalf("Revoked = %v with no error, want an error", revoked)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("the failed check took %v, want it bounded near %v", elapsed, lookupTimeout)
		}
	})
}

// newID returns a random version 4 UUID, the form user-service writes its keys
// with, so no two runs share keys.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
