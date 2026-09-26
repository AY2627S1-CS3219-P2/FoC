// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-09-26
// Scope: Tests for when refresh and logout expire the refresh cookie,
//   including a rejected or unreachable logout and a rejected refresh
//   (PR #4 review findings).
// Author review: nigeltzy - Checked the implementation of this test code, all seems normal and valid.

package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foc/api-gateway/internal/proxy"
)

// authRoute is the /auth → user-service mapping the router uses.
func authRoute(baseURL string) proxy.Route {
	return proxy.Route{BaseURL: baseURL, StripPrefix: "/auth", AddPrefix: "/api/v1/users"}
}

// cookieExpired reports whether the response expires the refresh cookie.
func cookieExpired(rec *httptest.ResponseRecorder) bool {
	for _, sc := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(sc, "foc_refresh=;") && strings.Contains(sc, "Max-Age=0") {
			return true
		}
	}
	return false
}

// TestSessionCookieFollowsTheDownstreamAnswer checks which user-service
// answers expire the refresh cookie. Logout always does, so a logout rejected
// for an expired access token cannot leave a cookie that signs the user back
// in. Refresh does on 401, so a dead token is not presented again, but not on
// a 5xx, where the session may still be live.
func TestSessionCookieFollowsTheDownstreamAnswer(t *testing.T) {
	t.Parallel()

	rotating := func(r proxy.Route) (*proxy.Proxy, error) { return proxy.NewSessionRotating(r, time.Hour) }

	tests := []struct {
		name        string
		newProxy    func(proxy.Route) (*proxy.Proxy, error)
		path        string
		status      int
		wantExpired bool
	}{
		{"logout 204", proxy.NewSessionEnding, "/auth/logout", http.StatusNoContent, true},
		{"logout 401, access token expired", proxy.NewSessionEnding, "/auth/logout", http.StatusUnauthorized, true},
		{"logout 500", proxy.NewSessionEnding, "/auth/logout", http.StatusInternalServerError, true},
		{"refresh 401", rotating, "/auth/refresh", http.StatusUnauthorized, true},
		{"refresh 500", rotating, "/auth/refresh", http.StatusInternalServerError, false},
		{"refresh 200", rotating, "/auth/refresh", http.StatusOK, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				if tt.status == http.StatusOK {
					_, _ = w.Write([]byte(`{"accessToken":"a","refreshToken":"rotated"}`))
				} else if tt.status != http.StatusNoContent {
					_, _ = w.Write([]byte(`{"error":"rejected"}`))
				}
			}))
			t.Cleanup(downstream.Close)

			p, err := tt.newProxy(authRoute(downstream.URL))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			req.AddCookie(&http.Cookie{Name: "foc_refresh", Value: "rt"})
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d passed through", rec.Code, tt.status)
			}
			if got := cookieExpired(rec); got != tt.wantExpired {
				t.Errorf("cookie expired = %v, want %v (Set-Cookie: %q)",
					got, tt.wantExpired, rec.Header().Values("Set-Cookie"))
			}
		})
	}
}

// TestLogoutExpiresTheCookieWhenUserServiceIsUnreachable checks the 502 path.
func TestLogoutExpiresTheCookieWhenUserServiceIsUnreachable(t *testing.T) {
	t.Parallel()

	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	p, err := proxy.NewSessionEnding(authRoute(down.URL))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "foc_refresh", Value: "rt"})
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if !cookieExpired(rec) {
		t.Errorf("cookie not expired on an unreachable logout (Set-Cookie: %q)", rec.Header().Values("Set-Cookie"))
	}
}

// TestUnreachableRefreshKeepsTheCookie checks that the 502 path leaves a
// refresh cookie alone.
func TestUnreachableRefreshKeepsTheCookie(t *testing.T) {
	t.Parallel()

	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	p, err := proxy.NewSessionRotating(authRoute(down.URL), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "foc_refresh", Value: "rt"})
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie = %q, want none", got)
	}
}

// AI-generated (edited by PENDING).
// TestRefreshForwardsANonJSONBodyUnchanged checks the path where the refresh
// cookie cannot be merged into the request body because the body is not a
// JSON object: the body is forwarded as it came, with a matching length.
func TestRefreshForwardsANonJSONBodyUnchanged(t *testing.T) {
	t.Parallel()

	var gotBody string
	var gotLength int64
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody, gotLength = string(raw), r.ContentLength
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(downstream.Close)

	p, err := proxy.NewSessionRotating(authRoute(downstream.URL), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader("not json"))
	req.AddCookie(&http.Cookie{Name: "foc_refresh", Value: "rt"})
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if gotBody != "not json" || gotLength != int64(len("not json")) {
		t.Errorf("downstream got body %q (Content-Length %d), want it unchanged", gotBody, gotLength)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want user-service's 400 passed through", rec.Code)
	}
}
