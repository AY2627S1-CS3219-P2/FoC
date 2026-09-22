// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Refresh-token cookie translation at the browser edge — moving the
//   token between user-service's JSON bodies and an HttpOnly cookie, so it
//   is never readable by page scripts.
// Author review: Nigeltzy - The AI was used to create the above, with human review and edits.
//   This idea was based on my understanding of HttpOnly cookies and security
//   considerations that I learned in my other CS mods. The AI helped generate the
//   boiler code functions, and I reviewed to check if they made sense.

package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// The refresh token's cookie. Its attributes are a recorded decision, not
// defaults picked here:
//
//	HttpOnly  page scripts cannot read it, so an XSS cannot steal the
//	          durable credential — only the in-memory access token, which
//	          expires in minutes.
//	Secure    HTTPS only. Browsers make an exception for localhost, so local
//	          development is unaffected.
//	Strict    the frontend and the gateway are same-origin (the browser
//	          talks only to the frontend's port, which proxies here), so
//	          nothing legitimate is cross-site.
//	Path      /auth covers refresh AND logout. Scoping it to /auth/refresh
//	          alone would stop the browser sending it to /auth/logout, and
//	          user-service's LogoutRequest requires the token.
const (
	refreshCookieName = "foc_refresh"
	refreshCookiePath = "/auth"
)

// refreshTokenField is the JSON key user-service uses on the wire, in both
// AuthResponse and LogoutRequest (its committed api/openapi.yaml).
const refreshTokenField = "refreshToken"

// newRefreshCookie returns the cookie carrying a freshly issued refresh token.
func newRefreshCookie(value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// expiredRefreshCookie returns the cookie that clears an existing one.
//
// Name, Path and the attributes must match what was set or the browser keeps
// the original alongside this one.
func expiredRefreshCookie() *http.Cookie {
	return &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// takeRefreshToken removes refreshToken from a JSON object body and returns it
// with the remaining body.
//
// The token must not reach the page: it goes into the cookie instead, and what
// the browser receives has no durable credential in it at all.
//
// A body that is not a JSON object, or carries no refreshToken, comes back
// unchanged with an empty token — an error response passes through untouched.
func takeRefreshToken(body []byte) (token string, rest []byte, err error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", body, nil
	}
	raw, ok := fields[refreshTokenField]
	if !ok {
		return "", body, nil
	}
	if err := json.Unmarshal(raw, &token); err != nil {
		return "", body, fmt.Errorf("proxy: %s is not a string: %w", refreshTokenField, err)
	}
	delete(fields, refreshTokenField)
	rest, err = json.Marshal(fields)
	if err != nil {
		return "", body, fmt.Errorf("proxy: re-encoding response body: %w", err)
	}
	return token, rest, nil
}

// putRefreshToken adds refreshToken to a JSON object body.
//
// This is what keeps user-service's contract unchanged: its LogoutRequest and
// RefreshRequest still require the field, and the browser no longer has it to
// send, so the gateway puts back what it took out. An empty or absent body
// becomes a fresh object rather than failing — the frontend deliberately sends
// no body on these routes now.
func putRefreshToken(body []byte, token string) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &fields); err != nil {
			return nil, fmt.Errorf("proxy: request body is not a JSON object: %w", err)
		}
	}
	encoded, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("proxy: encoding %s: %w", refreshTokenField, err)
	}
	fields[refreshTokenField] = encoded
	return json.Marshal(fields)
}

// readAndClose drains a body and closes it, so the caller can replace it.
func readAndClose(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// setJSONBody replaces a request or response body and keeps the length
// headers consistent. A stale Content-Length is not a cosmetic problem: the
// peer reads that many bytes and hangs or truncates.
func setJSONBody(header http.Header, body []byte) (io.ReadCloser, int64) {
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	return io.NopCloser(bytes.NewReader(body)), int64(len(body))
}
