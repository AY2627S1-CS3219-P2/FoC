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

// Package proxy forwards a verified request to one downstream service over
// synchronous REST, translating the token claims into HTTP headers (D-013).
//
// The strip in Rewrite is the security-critical line in this package. D-022
// lets downstream services trust ClaimHeaderUserID and ClaimHeaderRole
// precisely because a client cannot set them: whatever the caller sent is
// deleted, and only values derived from a verified token are written. Remove
// the strip and any caller can send "X-User-Role: ADMIN" with an ordinary
// token and be believed.
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

// Claim headers the gateway injects and downstream services read (D-022).
// supplier-service already reads ClaimHeaderRole as an interim stand-in.
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

// Proxy forwards to a single downstream service.
//
// One Proxy per callee, constructed with that callee's base URL. Adding a
// service to the gateway is a new Proxy and a route in internal/httpapi — no
// change in here (D-027).
type Proxy struct {
	reverse *httputil.ReverseProxy
	route   Route
	// behaviour distinguishes the constructors below. Set by them rather than
	// passed in, so callers pick a named function instead of handing over
	// flags that select a branch (root AGENTS.md §5).
	behaviour behaviour
}

// behaviour is the two independent choices a route makes about credentials.
//
// These were a single `passthrough bool` until user-service landed, which was
// only ever adequate because the two cases it named happened to differ in both
// respects at once. They are not the same question: whether the callee still
// needs the bearer token is about that service, and whether the gateway has a
// verified identity to assert is about the route.
type behaviour struct {
	// forwardToken leaves Authorization on the outbound request.
	forwardToken bool
	// injectClaims writes the verified identity into the claim headers.
	injectClaims bool
	// session is the refresh-token cookie translation, empty for every route
	// that does not carry one.
	session sessionBehaviour
}

// sessionBehaviour is what a route does about the refresh-token cookie.
//
// Set by the named constructors below, never passed in: a caller picks
// NewSessionRotating, it does not hand over three booleans that select
// branches in here (root AGENTS.md §5, control coupling).
type sessionBehaviour struct {
	// cookieFromBody moves refreshToken out of the RESPONSE body into a
	// Set-Cookie. Login and refresh, where user-service issues one.
	cookieFromBody bool
	// bodyFromCookie puts refreshToken back into the REQUEST body from the
	// cookie. Refresh and logout, whose contracts still require the field.
	bodyFromCookie bool
	// clearCookie expires the cookie on the response. Logout.
	clearCookie bool
	// ttl is the cookie's Max-Age, from config (must match user-service's
	// JWT_REFRESH_TOKEN_TTL).
	ttl time.Duration
}

// Route describes one public prefix and where it goes. Adding a service to
// the gateway is a new Route in internal/httpapi's table (D-027) — this
// package does not change.
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

// New returns a Proxy for an AUTHENTICATED route. The caller's token has
// already been verified, so the bearer token is stripped (downstream services
// do not parse JWTs, D-022) and the claim headers are injected.
//
// Returns a ready-to-use value; there is nothing to start afterwards
// (root AGENTS.md §5).
func New(route Route) (*Proxy, error) {
	return build(route, behaviour{forwardToken: false, injectClaims: true})
}

// NewRetainingToken returns a Proxy for an AUTHENTICATED route whose callee
// verifies the access token for itself.
//
// Same as New in every respect that matters to D-022 — client-supplied claim
// headers are still stripped, and the gateway's own verified values are still
// injected — except that Authorization survives the hop.
//
// This exists for user-service. It is the token ISSUER (D-012, D-023), and its
// own routes sit behind a RequireJWT middleware that reads the Authorization
// header; its api/openapi.yaml marks them `security: BearerAuth: []`. Under
// New they answer 401, because the gateway had deleted the very header they
// authenticate on.
//
// The credential is not being spread any wider than it already was: the token
// was minted by user-service and this hands it back to the service that signed
// it, not onward to a third party. Every other callee still gets New, so
// supplier-, order- and credit-service never see a bearer token.
//
// Recorded as D-022's one exception in ai/decisions.md (D-030), which also
// marks D-022 amended. Every other callee keeps New.
func NewRetainingToken(route Route) (*Proxy, error) {
	return build(route, behaviour{forwardToken: true, injectClaims: true})
}

// NewPassthrough returns a Proxy for a PUBLIC auth route — login, register,
// refresh, logout.
//
// These carry credentials rather than a verified identity, so the Authorization
// header is preserved: logout needs the access token to reach user-service so
// it can blocklist that jti. No claim headers are injected, because nothing
// has been verified here — user-service authenticates these itself.
func NewPassthrough(route Route) (*Proxy, error) {
	return build(route, behaviour{forwardToken: true, injectClaims: false})
}

// NewSessionIssuing returns a Proxy for POST /auth/login.
//
// user-service answers with {accessToken, refreshToken}. The refresh token is
// lifted out of that body into an HttpOnly cookie and never reaches the page,
// so a script that compromises the tab gets the access token -- which expires
// in minutes -- and not the durable credential.
func NewSessionIssuing(route Route, ttl time.Duration) (*Proxy, error) {
	return build(route, behaviour{
		forwardToken: true,
		session:      sessionBehaviour{cookieFromBody: true, ttl: ttl},
	})
}

// NewSessionRotating returns a Proxy for POST /auth/refresh.
//
// Both directions. user-service's RefreshRequest requires refreshToken in the
// body and the browser can no longer read it, so the gateway puts the cookie's
// value back in on the way out; the rotated token in the reply is lifted into
// a new cookie on the way back. user-service rotates on every exchange and
// treats a replay as a compromised session, so the cookie MUST be replaced
// here -- leaving the old one is a self-inflicted session revocation.
func NewSessionRotating(route Route, ttl time.Duration) (*Proxy, error) {
	return build(route, behaviour{
		forwardToken: true,
		session:      sessionBehaviour{bodyFromCookie: true, cookieFromBody: true, ttl: ttl},
	})
}

// NewSessionEnding returns a Proxy for POST /auth/logout.
//
// Injects the cookie's token into the body, because LogoutRequest requires it
// and the browser cannot supply it, then expires the cookie. The bearer token
// is forwarded too: user-service blocklists that access token's jti.
//
// This route is why the cookie's Path is /auth and not /auth/refresh -- the
// browser would not send it here otherwise, and logout would silently stop
// revoking anything.
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
			// SetXForwarded sets X-Forwarded-For/Host/Proto and, importantly,
			// drops whatever the client sent for them.
			r.SetXForwarded()

			// Applies to every route, including the passthrough ones — the
			// whole point of /auth/login is that it reaches user-service as
			// /api/v1/users/login.
			r.Out.URL.Path = p.route.RewritePath(r.In.URL.Path)

			// STRIP, unconditionally and on every route. Whatever the client
			// sent for these is deleted before anything is injected. This is
			// what D-022 rests on: a caller cannot assert its own identity.
			r.Out.Header.Del(ClaimHeaderUserID)
			r.Out.Header.Del(ClaimHeaderRole)

			if !p.behaviour.forwardToken {
				// Most downstream services do not parse JWTs (D-022), so
				// forwarding the bearer token would spread a credential for no
				// purpose. user-service is the exception — see
				// NewRetainingToken.
				r.Out.Header.Del("Authorization")
			}

			// The refresh token travels as a cookie now, so put it back in
			// the body user-service's contract still asks for. Failure is
			// deliberately silent: no cookie means no session, and
			// user-service answers 401 on the missing field, which is the
			// right answer and its to give.
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

			// INJECT. Only values derived from a verified token reach here.
			// Skipped on the public auth routes, where nothing has been
			// verified yet and there is no identity to assert.
			if p.behaviour.injectClaims {
				if id, ok := IdentityFrom(r.In.Context()); ok {
					r.Out.Header.Set(ClaimHeaderUserID, id.UserID)
					r.Out.Header.Set(ClaimHeaderRole, id.Role)
				}
			}
		},
		// Downstream services set their own CORS headers — supplier-service
		// runs a permissive cors.Handler of its own, from when the browser
		// reached it directly. ReverseProxy copies response headers through,
		// so the browser would receive TWO Access-Control-Allow-Origin values
		// and reject the response outright: "the header contains multiple
		// values '*, *', but only one is allowed". fetch() then rejects, and
		// the UI reports it as the service being unreachable.
		//
		// curl cannot catch this — it ignores CORS entirely. Only a browser
		// sees it.
		//
		// The gateway is the public edge and owns the CORS policy alone
		// (internal/httpapi's interimCORS), so whatever a callee said about
		// origins is discarded here.
		ModifyResponse: func(res *http.Response) error {
			for name := range res.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
					res.Header.Del(name)
				}
			}

			// Only on success. An error reply carries no token to lift, and
			// clearing the cookie on a failed logout would sign the user out
			// of a session the server still considers live.
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

			if p.behaviour.session.clearCookie {
				res.Header.Add("Set-Cookie", expiredRefreshCookie().String())
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// A downstream being unreachable is the gateway's problem to
			// report, not a 500 from an unknown source.
			log.Printf("api-gateway: proxy %s %s: %v", r.Method, r.URL.Path, err)
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
// Exported because it is the observable contract of a Route, and the route
// table in internal/httpapi is only correct if this is.
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
