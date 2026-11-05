// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Table-driven tests of the panic recoverer: one JSON ERROR line with the
//   request id and the whole stack in one field, a JSON 500 only while no status
//   has gone out, and http.ErrAbortHandler handed back to net/http (D-135).
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverPanics(t *testing.T) {
	cases := []struct {
		name        string
		handler     http.HandlerFunc
		wantStatus  int
		wantBody    string // JSON; "" for an empty body
		wantPanic   string // the logged panic value; "" when no panic line may be written
		wantRepanic bool   // the panic must reach net/http
	}{
		{
			name:       "a panic before anything is sent is a JSON 500",
			handler:    func(http.ResponseWriter, *http.Request) { panic("boom") },
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}`,
			wantPanic:  "boom",
		},
		{
			name: "a panic after the status went out keeps that status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				panic(errors.New("late"))
			},
			wantStatus: http.StatusAccepted,
			wantPanic:  "late",
		},
		{
			name:        "an aborted response is panicked on to the server",
			handler:     func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) },
			wantRepanic: true,
		},
		{
			name:       "a handler that does not panic",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			wantStatus: http.StatusNoContent,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			// The router's order: the access log's writer is how the recoverer
			// knows whether a status already went out.
			h := middleware.RequestID(accessLog(log)(recoverPanics(log)(tc.handler)))
			req := httptest.NewRequest(http.MethodGet, "/suppliers", nil)
			req.Header.Set("X-Request-Id", "req-panic")
			rec := httptest.NewRecorder()

			if tc.wantRepanic {
				assert.PanicsWithValue(t, http.ErrAbortHandler, func() { h.ServeHTTP(rec, req) })
				assert.Empty(t, logs.String(), "net/http logs nothing for an aborted response")
				return
			}
			h.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantBody == "" {
				assert.Empty(t, rec.Body.String())
			} else {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
			}
			var panics []map[string]any
			sc := bufio.NewScanner(&logs)
			sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
			for sc.Scan() {
				var line map[string]any
				require.NoError(t, json.Unmarshal(sc.Bytes(), &line), "every entry is one JSON line: %s", sc.Text())
				if line["msg"] == "panic serving request" {
					panics = append(panics, line)
				}
			}
			require.NoError(t, sc.Err())
			if tc.wantPanic == "" {
				assert.Empty(t, panics)
				return
			}
			require.Len(t, panics, 1)
			assert.Equal(t, "ERROR", panics[0]["level"])
			assert.Equal(t, "req-panic", panics[0]["request_id"])
			assert.Equal(t, tc.wantPanic, panics[0]["panic"])
			stack, _ := panics[0]["stack"].(string)
			assert.True(t, strings.HasPrefix(stack, "goroutine "), "the whole stack in one field, got %q", stack)
		})
	}
}
