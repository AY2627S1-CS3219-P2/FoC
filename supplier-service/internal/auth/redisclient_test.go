// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Tests that the production Redis client honours the revocation
//   lookup's 500 ms deadline against a server that accepts the connection and
//   never answers, and that a bad URL's error does not quote it.
// Author review: PENDING — <reviewer to complete>

package auth

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stalledRedis listens, accepts every connection and reads without ever
// replying, so a client waits on its socket instead of failing to connect.
func stalledRedis(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					if _, err := conn.Read(buf); err != nil {
						return
					}
				}
			}()
		}
	}()
	return l.Addr().String()
}

func TestAStalledRedisIsGivenUpOnAtTheRevocationDeadline(t *testing.T) {
	// go-redis's default socket timeout is 3 s, so without ContextTimeoutEnabled
	// this lookup would take about that long, not revocationTimeout.
	rdb, err := NewRedisClient("redis://" + stalledRedis(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdb.Close() })

	start := time.Now()
	_, err = NewRevocations(rdb).Revoked(context.Background(), Claims{UserID: uuid.New(), TokenID: "j"})
	elapsed := time.Since(start)

	require.ErrorIs(t, err, ErrRedisUnreachable)
	assert.Less(t, elapsed, 2*time.Second, "the lookup must stop at the revocation deadline, not the socket timeout")
}

func TestNewRedisClientNeverQuotesAMalformedURL(t *testing.T) {
	_, err := NewRedisClient("tcp://:hunter2@redis:6379")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2")
}
