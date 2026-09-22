// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Added focused tests for the recorded Redis suspension-key writer.
// Author review: PENDING — reviewer to complete

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRedisBlocklistWriterStoresSuspensionKeyTimestampAndTTL(t *testing.T) {
	client := &fakeRedisSetter{}
	writer := NewRedisBlocklistWriter(client)
	uid := uuid.New()
	suspendedAt := time.Date(2026, 9, 22, 5, 30, 12, 345678900, time.FixedZone("SGT", 8*60*60))
	ttl := 15 * time.Minute

	if err := writer.WriteSuspension(context.Background(), uid, suspendedAt, ttl); err != nil {
		t.Fatal(err)
	}

	if client.key != "suspended:uid:"+uid.String() {
		t.Fatalf("Redis key = %q, want suspension key", client.key)
	}
	if client.value != suspendedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("Redis value = %#v, want UTC suspension timestamp", client.value)
	}
	if client.ttl != ttl {
		t.Fatalf("Redis TTL = %v, want %v", client.ttl, ttl)
	}
}

func TestRedisBlocklistWriterPreservesSuspensionSetErrors(t *testing.T) {
	setFailure := errors.New("Redis unavailable")
	writer := NewRedisBlocklistWriter(&fakeRedisSetter{err: setFailure})

	err := writer.WriteSuspension(context.Background(), uuid.New(), time.Now(), time.Minute)
	if !errors.Is(err, setFailure) {
		t.Fatalf("error = %v, want Redis failure", err)
	}
}

func TestRedisBlocklistWriterRejectsInvalidSuspensionInputs(t *testing.T) {
	writer := NewRedisBlocklistWriter(&fakeRedisSetter{})
	tests := []struct {
		name string
		uid  uuid.UUID
		at   time.Time
		ttl  time.Duration
	}{
		{name: "nil account ID", uid: uuid.Nil, at: time.Now(), ttl: time.Minute},
		{name: "missing timestamp", uid: uuid.New(), ttl: time.Minute},
		{name: "zero TTL", uid: uuid.New(), at: time.Now()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := writer.WriteSuspension(context.Background(), tt.uid, tt.at, tt.ttl); err == nil {
				t.Fatal("invalid suspension input was accepted")
			}
		})
	}
}
