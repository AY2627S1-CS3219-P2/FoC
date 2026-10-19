//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-19
// Scope: Added Redis-container coverage for the access-token and suspension
//   blocklist writer/reader contract.
// Author review: PENDING — reviewer to complete

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisBlocklistWriterAndReader(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start Redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate Redis container: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%s", host, port.Port())})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	writer := NewRedisBlocklistWriter(client)
	reader := NewRedisBlocklistReader(client)
	uid, jti := uuid.New(), uuid.New()
	suspendedAt := time.Now().UTC().Truncate(time.Second)
	if err := writer.BlockAccessToken(ctx, jti, time.Minute); err != nil {
		t.Fatal(err)
	}
	if revoked, err := reader.IsRevoked(ctx, jti, uid, suspendedAt.Add(time.Minute)); err != nil || !revoked {
		t.Fatalf("JTI revocation = %t, %v; want true, nil", revoked, err)
	}

	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSuspension(ctx, uid, suspendedAt, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		issuedAt    time.Time
		wantRevoked bool
	}{
		{name: "before suspension", issuedAt: suspendedAt.Add(-time.Second), wantRevoked: true},
		{name: "at suspension", issuedAt: suspendedAt, wantRevoked: true},
		{name: "after suspension", issuedAt: suspendedAt.Add(time.Second), wantRevoked: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := reader.IsRevoked(ctx, uuid.New(), uid, test.issuedAt)
			if err != nil || got != test.wantRevoked {
				t.Fatalf("IsRevoked() = %t, %v; want %t, nil", got, err, test.wantRevoked)
			}
		})
	}
	if err := container.Stop(ctx, nil); err != nil {
		t.Fatalf("stop Redis container: %v", err)
	}
	if _, err := reader.IsRevoked(ctx, uuid.New(), uid, suspendedAt.Add(time.Second)); err == nil {
		t.Fatal("reader accepted a stopped Redis container")
	}
}
