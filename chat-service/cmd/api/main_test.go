// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Table-driven tests that net/http's own errors, through the server
//   newHTTPServer builds, reach the JSON logger as one ERROR line each, and
//   that a Redis or RabbitMQ URL that does not parse keeps its password out
//   of the log (D-135).
// Author review: James Teo — reviewed and verified.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"foc/chat-service/internal/consumer"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetHTTPErrorsAreOneJSONErrorLine(t *testing.T) {
	cases := []struct {
		name    string
		printed string // what net/http hands its ErrorLog
	}{
		{name: "an accept error", printed: "http: Accept error: accept tcp [::]:8085: accept4: too many open files; retrying in 5ms"},
		{name: "a write on a hijacked connection", printed: "http: response.Write on hijacked connection from foc/chat-service/internal/httpapi.(*Handler).ServeWS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			server := newHTTPServer(":0", http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&logs, nil)))

			server.ErrorLog.Print(tc.printed)

			lines := strings.Split(strings.TrimSuffix(logs.String(), "\n"), "\n")
			require.Len(t, lines, 1, logs.String())
			var line map[string]any
			require.NoError(t, json.Unmarshal([]byte(lines[0]), &line), lines[0])
			assert.Equal(t, "ERROR", line["level"])
			assert.Equal(t, tc.printed, line["msg"])
		})
	}
}

func TestRedactURLErrorKeepsThePasswordOut(t *testing.T) {
	const password = "hunter2%zz" // %zz is no valid escape, so the URL does not parse
	_, redisErr := redis.ParseURL("redis://:" + password + "@chat-redis:6379/0")
	amqpErr := consumer.RunConsumer(context.Background(), "amqp://chat:"+password+"@rabbitmq:5672/foc", nil)
	refused := errors.New("dial amqp: dial tcp 10.0.0.7:5672: connect: connection refused")

	cases := []struct {
		name  string
		err   error
		leaks bool // the error as returned quotes the password
		want  string
	}{
		{name: "a Redis URL that does not parse", err: redisErr, leaks: true, want: "not a valid URL"},
		{name: "a RabbitMQ URL that does not parse", err: amqpErr, leaks: true, want: "not a valid URL"},
		{name: "an error that quotes no URL is kept", err: refused, want: refused.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, tc.err)
			if tc.leaks {
				require.Contains(t, tc.err.Error(), "hunter2", "without redaction the password would be logged")
			}

			got := redactURLError(tc.err)

			assert.Equal(t, tc.want, got.Error())
			assert.NotContains(t, got.Error(), "hunter2")
		})
	}
}
