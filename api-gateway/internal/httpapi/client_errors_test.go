// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests for POST /api/client-errors: the reports it takes and refuses,
//   and the one log line a report becomes.
// Author review: James Teo — reviewed and verified.

package httpapi_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// postReport sends body to POST /api/client-errors, with token as the bearer
// token unless it is empty.
func postReport(h *harness, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/client-errors", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return h.do(req)
}

// reportOfSize returns a valid report whose body is exactly n bytes.
func reportOfSize(t *testing.T, n int) string {
	t.Helper()
	const head, tail = `{"kind":"error","message":"`, `","stack":"","view":"board"}`
	fill := n - len(head) - len(tail)
	if fill < 0 {
		t.Fatalf("a report cannot be as short as %d bytes", n)
	}
	return head + strings.Repeat("x", fill) + tail
}

// A signed-in browser's report is answered 204 and logged as one WARN line
// naming the caller, and it never leaves the gateway.
func TestClientErrorReportIsLoggedAndAnswered204(t *testing.T) {
	h := newHarness(t)
	token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
	report := map[string]string{
		"kind":    "render",
		"message": "Cannot read properties of undefined (reading 'reward')",
		"stack":   "TypeError: Cannot read properties of undefined (reading 'reward')\n    at ErrandCard (ErrandCard.tsx:41:7)",
		"view":    "errand-board",
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	got := postReport(h, token, string(body))

	if got.Code != http.StatusNoContent || got.Body.Len() != 0 {
		t.Fatalf("status = %d, body %q; want 204 and no body", got.Code, got.Body.String())
	}
	id := got.Header().Get("X-Request-Id")
	if id == "" {
		t.Fatal("the response carries no X-Request-Id")
	}
	line := h.logs.only(t, msgBrowserError)
	want := map[string]string{"level": "WARN", "source": "browser", "user_id": "uid-123", "request_id": id}
	for key, value := range report {
		want[key] = value
	}
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %#v, want %q", key, line[key], value)
		}
	}
	if calls := h.downstreamCalls(); len(calls) != 0 {
		t.Errorf("the report reached %v; it must never be proxied", calls)
	}
}

// A report is one JSON object with only kind, message, stack and view, each
// a string, of a kind the browser sends, in at most 8 KB.
func TestClientErrorReportBody(t *testing.T) {
	const full = `{"kind":"error","message":"m","stack":"s","view":"board"}`
	cases := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"a full report", full, http.StatusNoContent},
		{"fields left out are empty", `{"kind":"rejection","message":"string"}`, http.StatusNoContent},
		{"exactly 8 KB", reportOfSize(t, 8<<10), http.StatusNoContent},
		{"over 8 KB", reportOfSize(t, 8<<10+1), http.StatusRequestEntityTooLarge},
		{"not JSON", `kind=error&message=m`, http.StatusBadRequest},
		{"empty", ``, http.StatusBadRequest},
		{"cut off", `{"kind":"error","message":"m"`, http.StatusBadRequest},
		{"not an object", `["error","m"]`, http.StatusBadRequest},
		{"null", `null`, http.StatusBadRequest},
		{"a field that is not a string", `{"kind":"error","message":42,"stack":"s","view":"board"}`, http.StatusBadRequest},
		{"a second value after the object", full + `{}`, http.StatusBadRequest},
		{"an unknown field", `{"kind":"error","message":"m","stack":"s","view":"board","userId":"someone-else"}`, http.StatusBadRequest},
		{"a field spelt in another case", `{"kind":"error","Message":"m","stack":"s","view":"board"}`, http.StatusBadRequest},
		{"an unknown kind", `{"kind":"warning","message":"m","stack":"s","view":"board"}`, http.StatusBadRequest},
		{"no kind", `{"message":"m","stack":"s","view":"board"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			token := h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})

			got := postReport(h, token, tc.body)

			if got.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", got.Code, tc.wantStatus, got.Body.String())
			}
			logged := h.logs.withMsg(t, msgBrowserError)
			if tc.wantStatus == http.StatusNoContent {
				if len(logged) != 1 {
					t.Errorf("%d browser error lines, want 1", len(logged))
				}
				return
			}
			if len(logged) != 0 {
				t.Errorf("a refused report was logged: %v", logged)
			}
			if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want the gateway's JSON error", ct)
			}
		})
	}
}

// Over a real connection, a report over 8 KB gets its 413 and the connection
// is closed, so the gateway reads no more of a body it will not use, while a
// normal report gets its 204 and keeps the connection. Either way the access
// line records the report's status.
func TestAnOversizedReportDropsItsConnection(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantClosed bool
	}{
		{"a report over 8 KB is refused and its connection closed", reportOfSize(t, 8<<10+1), http.StatusRequestEntityTooLarge, true},
		{"a normal report keeps its connection", `{"kind":"error","message":"m","stack":"s","view":"board"}`, http.StatusNoContent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			server := httptest.NewServer(h.router)
			t.Cleanup(server.Close)
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			// Bounds every read below, so a gateway that neither answers nor
			// hangs up fails the test instead of stalling it.
			if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatalf("set deadline: %v", err)
			}
			reader := bufio.NewReader(conn)
			report, err := http.NewRequest(http.MethodPost, server.URL+"/api/client-errors", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			report.Header.Set("Content-Type", "application/json")
			report.Header.Set("Authorization", "Bearer "+h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"}))
			health, err := http.NewRequest(http.MethodGet, server.URL+"/healthz", nil)
			if err != nil {
				t.Fatal(err)
			}

			res, err := sendOn(conn, reader, report)
			if err != nil {
				t.Fatalf("report: %v", err)
			}
			if res.StatusCode != tc.wantStatus || res.Close != tc.wantClosed {
				t.Fatalf("status = %d, Close = %t; want %d, %t", res.StatusCode, res.Close, tc.wantStatus, tc.wantClosed)
			}
			_, err = sendOn(conn, reader, health)
			if answered := err == nil; answered == tc.wantClosed {
				t.Fatalf("second request on the same connection answered = %t (err = %v), want %t", answered, err, !tc.wantClosed)
			}

			_ = conn.Close()
			server.Close() // returns once the server is done, so the access lines are in logs
			var reports []map[string]any
			for _, line := range h.logs.withMsg(t, msgAccess) {
				if line["path"] == "/api/client-errors" {
					reports = append(reports, line)
				}
			}
			if len(reports) != 1 || reports[0]["status"] != float64(tc.wantStatus) {
				t.Errorf("access lines for the report = %v, want one with status %d", reports, tc.wantStatus)
			}
		})
	}
}

// sendOn writes req on conn and reads its whole response from reader, so every
// request a test sends travels on that one connection.
func sendOn(conn net.Conn, reader *bufio.Reader, req *http.Request) (*http.Response, error) {
	if err := req.Write(conn); err != nil {
		return nil, err
	}
	res, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	_, err = io.Copy(io.Discard, res.Body)
	return res, err
}

// Only a signed-in caller can report: without a valid access token the report
// is refused before it is read, and nothing is logged as a browser error.
func TestClientErrorReportNeedsAToken(t *testing.T) {
	cases := []struct {
		name  string
		token func(t *testing.T, h *harness) string
	}{
		{"no token", func(*testing.T, *harness) string { return "" }},
		{"refresh token presented as bearer", func(t *testing.T, h *harness) string {
			return h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT", typ: "refresh"})
		}},
		{"revoked token", func(t *testing.T, h *harness) string {
			h.revocations.revoked = true
			return h.issuer.mint(t, tokenOpts{subject: "uid-123", role: "STUDENT"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			got := postReport(h, tc.token(t, h), `{"kind":"error","message":"m","stack":"s","view":"board"}`)

			if got.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", got.Code, got.Body.String())
			}
			if logged := h.logs.withMsg(t, msgBrowserError); len(logged) != 0 {
				t.Errorf("a report without a valid token was logged: %v", logged)
			}
		})
	}
}
