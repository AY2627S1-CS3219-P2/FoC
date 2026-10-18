//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Integration test of the revocation check against a real Redis that
//   requires a password (D-054, D-056): keys in user-service's formats refuse
//   a token, an error reply such as NOAUTH or WRONGPASS refuses the request
//   with 503, and a Redis that cannot be reached is skipped and logged.
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"foc/order-service/internal/auth"
)

// redisPassword is the password the test's Redis requires.
const redisPassword = "revocation-test-password"

// TestRevocationOverRedis runs the router's revocation check through
// auth.Revocations against a real Redis, so what a fake cannot show is
// covered: user-service's key formats, the error replies a misconfigured
// REDIS_URL gets back, and an address where nothing answers.
func TestRevocationOverRedis(t *testing.T) {
	t.Parallel()
	addr := startRedis(t)
	h := newHarness(t)
	routerOver := func(t *testing.T, opts *redis.Options) http.Handler {
		t.Helper()
		rdb := redis.NewClient(opts)
		t.Cleanup(func() { _ = rdb.Close() })
		return h.routerWith(ports{revocations: auth.NewRevocations(rdb)})
	}
	// writer stands in for user-service, the keys' only writer (D-054).
	writer := redis.NewClient(&redis.Options{Addr: addr, Password: redisPassword})
	t.Cleanup(func() { _ = writer.Close() })
	ctx := context.Background()

	t.Run("keys in user-service's formats refuse the token", func(t *testing.T) {
		router := routerOver(t, &redis.Options{Addr: addr, Password: redisPassword})
		loggedOut := tokenFor(student().UserID.String(), "STUDENT")
		require.Equal(t, http.StatusOK, serve(t, router, http.MethodGet, "/open", nil, withToken(loggedOut)).Code)

		jti := strings.Split(loggedOut, "|")[3]
		require.NoError(t, writer.Set(ctx, "jti:"+jti, "1", time.Minute).Err())
		requireError(t, serve(t, router, http.MethodGet, "/open", nil, withToken(loggedOut)), http.StatusUnauthorized, msgNoIdentity)

		suspended := student()
		// fakeTokens issues every token at startTime(); the suspension came later.
		suspendedAt := startTime().Add(time.Second).UTC().Format(time.RFC3339Nano)
		require.NoError(t, writer.Set(ctx, "suspended:uid:"+suspended.UserID.String(), suspendedAt, time.Minute).Err())
		requireError(t, serve(t, router, http.MethodGet, "/open", nil, as(suspended)), http.StatusUnauthorized, msgNoIdentity)
	})

	t.Run("an error reply refuses the request with 503", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			password string
			reply    string
		}{
			{"no password", "", "NOAUTH"},
			{"a wrong password", "not-the-password", "WRONGPASS"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				router := routerOver(t, &redis.Options{Addr: addr, Password: tc.password})
				requestID := "redis-" + strings.ReplaceAll(tc.name, " ", "-")

				rec := serve(t, router, http.MethodGet, "/open", nil, as(student()), withRequestID(requestID))

				requireError(t, rec, http.StatusServiceUnavailable, "sign-in cannot be checked right now; please try again")
				line := h.findLog(t, func(l map[string]any) bool {
					return l["level"] == "ERROR" && l["request_id"] == requestID
				})
				assert.Contains(t, line["err"], tc.reply)
			})
		}
	})

	t.Run("a Redis that cannot be reached is skipped and logged", func(t *testing.T) {
		router := routerOver(t, &redis.Options{Addr: closedAddress(t), DialTimeout: 200 * time.Millisecond, MaxRetries: -1})
		// Revoked in the real Redis, which this router cannot reach (D-056).
		token := tokenFor(student().UserID.String(), "STUDENT")
		require.NoError(t, writer.Set(ctx, "jti:"+strings.Split(token, "|")[3], "1", time.Minute).Err())

		rec := serve(t, router, http.MethodGet, "/open", nil, withToken(token), withRequestID("redis-unreachable"))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		line := h.findLog(t, func(l map[string]any) bool {
			return l["level"] == "ERROR" && l["request_id"] == "redis-unreachable"
		})
		assert.Contains(t, line["msg"], "revocation check skipped")
	})
}

// startRedis starts redis:7-alpine requiring redisPassword, and returns its
// address.
func startRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			Cmd:          []string{"redis-server", "--requirepass", redisPassword},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err, "start Redis container")
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379/tcp")
	require.NoError(t, err)
	return net.JoinHostPort(host, port.Port())
}

// closedAddress returns a loopback address nothing listens on, so a dial to
// it is refused.
func closedAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}
