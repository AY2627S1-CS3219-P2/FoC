// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Table-driven test that net/http's own errors, through the server
//   newHTTPServer builds, reach the JSON logger as one ERROR line each (D-135).
// Author review: James Teo — reviewed and verified.

package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetHTTPErrorsAreOneJSONErrorLine(t *testing.T) {
	cases := []struct {
		name    string
		printed string // what net/http hands its ErrorLog
	}{
		{name: "an accept error", printed: "http: Accept error: accept tcp [::]:8082: accept4: too many open files; retrying in 5ms"},
		{name: "a panic net/http caught, with its stack", printed: "http: panic serving 10.0.0.7:51234: boom\ngoroutine 7 [running]:\nnet/http.(*conn).serve.func1()"},
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
