// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Reverse proxy to one downstream service, including the header
//   strip-and-inject that D-022 depends on. 2026-09-21: the single
//   passthrough flag became two independent choices, and NewRetainingToken
//   was added for user-service, which verifies the bearer token itself.
//   2026-09-22: comment only — NewRetainingToken's "NOT RECORDED" note
//   replaced with a pointer to D-030, which now records it. Later that day:
//   ModifyResponse added, to drop CORS headers a callee sets for itself.
//   Then: the refresh token moved into an HttpOnly cookie the gateway owns,
//   translated to and from user-service's JSON bodies here.
// Author review: Nigeltzy - The AI was used to create the above, with human review and edits.
//   The generated code follows the framework and intent of what was discussed with me
//   and my teammate. Other than it, the AI was told to used default implementation
//   patterns for reverse proxies, and to use the existing codebase as a reference.

// Package proxy forwards a request to one downstream service over HTTP. It
// deletes any client-supplied ClaimHeaderUserID and ClaimHeaderRole headers
// and, on authenticated routes, sets them from the verified identity.
package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Claim headers the gateway sets from the verified token for downstream
// services to read.
const (
	ClaimHeaderUserID = "X-User-Id"
	ClaimHeaderRole   = "X-User-Role"
)

// Identity is what the gateway asserts about a caller. It is built from
// verified token claims and never from anything the caller sent.
type Identity struct {
	UserID string
	Role   string
}

// Proxy forwards requests for one Route to its downstream service.
type Proxy struct {
	reverse *httputil.ReverseProxy
	route   Route
	// behaviour is set only by the New* constructors.
	behaviour behaviour
}

// behaviour is what a route does with credentials: whether Authorization is
// forwarded, whether the verified identity is injected, and how the refresh
// cookie is handled.
type behaviour struct {
	// forwardToken leaves Authorization on the outbound request.
	forwardToken bool
	// injectClaims writes the verified identity into the claim headers.
	injectClaims bool
	// session is the refresh-token cookie translation, empty for every route
	// that does not carry one.
	session sessionBehaviour
}

// sessionBehaviour is what a route does with the refresh-token cookie.
type sessionBehaviour struct {
	// cookieFromBody moves refreshToken from the response body into a
	// Set-Cookie (login and refresh).
	cookieFromBody bool
	// bodyFromCookie copies the cookie's value into the request body's
	// refreshToken field (refresh and logout).
	bodyFromCookie bool
	// clearCookie expires the cookie on every response, whatever user-service
	// answered. Logout.
	clearCookie bool
	// AI-generated (edited by nigeltzy).
	// clearOnReject expires the cookie when user-service answers 401, so a
	// dead refresh token is not presented again. Refresh.
	clearOnReject bool
	// ttl is the cookie's Max-Age, from config (must match user-service's
	// JWT_REFRESH_TOKEN_TTL).
	ttl time.Duration
}

// Route describes one public path prefix and the downstream service it maps to.
type Route struct {
	// BaseURL is the downstream service, e.g. "http://order-service:8083".
	BaseURL string
	// StripPrefix is removed from the front of the public path.
	StripPrefix string
	// AddPrefix is prepended to what remains. Together these let the public
	// URL differ from the service's own routes: /auth/login reaches
	// user-service as /api/v1/users/login by stripping "/auth" and adding
	// "/api/v1/users".
	AddPrefix string
}

// New returns a Proxy for an authenticated route. It deletes the
// Authorization header and injects the verified identity into the claim
// headers.
func New(route Route) (*Proxy, error) {
	return build(route, behaviour{forwardToken: false, injectClaims: true})
}

// NewRetainingToken returns a Proxy like New that also forwards the
// Authorization header, for user-service, which verifies the access token
// itself. Client-supplied claim headers are still stripped and the verified
// ones injected. Only user-service's route may use it; every other callee uses
// New and never receives the bearer token (D-030 in ai/decisions.md).
func NewRetainingToken(route Route) (*Proxy, error) {
	return build(route, behaviour{forwardToken: true, injectClaims: true})
}

// NewPassthrough returns a Proxy for a public route with no verified identity
// and no refresh cookie, such as POST /auth/register. Authorization is
// forwarded unchanged and no claim headers are injected.
func NewPassthrough(route Route) (*Proxy, error) {
	return build(route, behaviour{forwardToken: true, injectClaims: false})
}

// NewSessionIssuing returns a Proxy for POST /auth/login. It moves
// refreshToken from user-service's response body into the HttpOnly refresh
// cookie, so the page never receives it.
func NewSessionIssuing(route Route, ttl time.Duration) (*Proxy, error) {
	return build(route, behaviour{
		forwardToken: true,
		session:      sessionBehaviour{cookieFromBody: true, ttl: ttl},
	})
}

// NewSessionRotating returns a Proxy for POST /auth/refresh. It copies the
// refresh cookie into the request body's refreshToken field and replaces the
// cookie with the rotated token from a successful response. user-service
// revokes all of a user's sessions when an old refresh token is reused, so the
// cookie must be replaced on every refresh, and expired when user-service
// rejects it with 401. A 5xx leaves the cookie in place.
func NewSessionRotating(route Route, ttl time.Duration) (*Proxy, error) {
	return build(route, behaviour{
		forwardToken: true,
		session:      sessionBehaviour{bodyFromCookie: true, cookieFromBody: true, clearOnReject: true, ttl: ttl},
	})
}

// NewSessionEnding returns a Proxy for POST /auth/logout. It copies the
// refresh cookie into the request body's refreshToken field, forwards
// Authorization so user-service can revoke the access token, and expires the
// cookie whatever user-service answers, including when it cannot be reached.
// A logout rejected because the access token expired while the user was idle
// must still leave the browser without the cookie; otherwise the next page
// load refreshes and signs the user back in.
func NewSessionEnding(route Route) (*Proxy, error) {
	return build(route, behaviour{
		forwardToken: true,
		session:      sessionBehaviour{bodyFromCookie: true, clearCookie: true},
	})
}

func build(route Route, how behaviour) (*Proxy, error) {
	target, err := url.Parse(route.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: parsing base URL %q: %w", route.BaseURL, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("proxy: base URL %q needs a scheme and host", route.BaseURL)
	}

	p := &Proxy{route: route, behaviour: how}
	p.reverse = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// The client's X-Forwarded-* headers are removed before Rewrite runs;
			// SetXForwarded sets fresh ones from the inbound request.
			r.SetXForwarded()

			r.Out.URL.Path = p.route.RewritePath(r.In.URL.Path)

			// Delete client-supplied claim headers on every route before anything is
			// injected. Downstream services trust these headers, so without this delete a
			// caller could send X-User-Role: ADMIN and be believed (D-022 in ai/decisions.md).
			r.Out.Header.Del(ClaimHeaderUserID)
			r.Out.Header.Del(ClaimHeaderRole)

			if !p.behaviour.forwardToken {
				r.Out.Header.Del("Authorization")
			}

			// Copy the refresh cookie into the JSON body's refreshToken field. Without a
			// cookie the body is forwarded unchanged and user-service rejects the request.
			if p.behaviour.session.bodyFromCookie {
				if c, err := r.In.Cookie(refreshCookieName); err == nil && c.Value != "" {
					if raw, err := readAndClose(r.Out.Body); err == nil {
						if merged, err := putRefreshToken(raw, c.Value); err == nil {
							r.Out.Body, r.Out.ContentLength = setJSONBody(r.Out.Header, merged)
						} else {
							log.Printf("api-gateway: refresh cookie into body: %v", err)
							r.Out.Body, r.Out.ContentLength = setJSONBody(r.Out.Header, raw)
						}
					}
				}
			}

			// Inject the verified identity. Routes built without injectClaims (the
			// public auth routes) send no claim headers.
			if p.behaviour.injectClaims {
				if id, ok := IdentityFrom(r.In.Context()); ok {
					r.Out.Header.Set(ClaimHeaderUserID, id.UserID)
					r.Out.Header.Set(ClaimHeaderRole, id.Role)
				}
			}
		},
		// Drop any Access-Control-* headers a downstream service sets; the gateway
		// sends no CORS headers, so none reach the browser.
		ModifyResponse: func(res *http.Response) error {
			for name := range res.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
					res.Header.Del(name)
				}
			}

			// AI-generated (edited by nigeltzy).
			if p.behaviour.session.clearCookie ||
				(p.behaviour.session.clearOnReject && res.StatusCode == http.StatusUnauthorized) {
				res.Header.Add("Set-Cookie", expiredRefreshCookie().String())
			}

			// An error reply carries no token to lift.
			if res.StatusCode < 200 || res.StatusCode > 299 {
				return nil
			}

			if p.behaviour.session.cookieFromBody {
				raw, err := readAndClose(res.Body)
				if err != nil {
					return fmt.Errorf("proxy: reading auth response: %w", err)
				}
				token, rest, err := takeRefreshToken(raw)
				if err != nil {
					return err
				}
				if token != "" {
					res.Header.Add("Set-Cookie", newRefreshCookie(token, p.behaviour.session.ttl).String())
				}
				res.Body, res.ContentLength = setJSONBody(res.Header, rest)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Answer 502 with a JSON error body when the downstream cannot be reached.
			log.Printf("api-gateway: proxy %s %s: %v", r.Method, r.URL.Path, err)
			// AI-generated (edited by nigeltzy).
			if p.behaviour.session.clearCookie {
				w.Header().Add("Set-Cookie", expiredRefreshCookie().String())
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream service unavailable"}`))
		},
		Transport: &http.Transport{
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}
	return p, nil
}

// RewritePath maps a public path to the downstream service's own path.
func (r Route) RewritePath(path string) string {
	trimmed := strings.TrimPrefix(path, r.StripPrefix)
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	out := r.AddPrefix + trimmed
	// A bare prefix ("/api/suppliers") leaves "/" behind; AddPrefix alone is
	// the right target in that case, not AddPrefix + "/".
	if trimmed == "/" && r.AddPrefix != "" {
		out = r.AddPrefix
	}
	return out
}

// ServeHTTP forwards the request to the downstream service.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.reverse.ServeHTTP(w, r)
}
