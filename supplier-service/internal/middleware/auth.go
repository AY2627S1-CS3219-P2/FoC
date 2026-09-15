// Package middleware provides request-level guards for the Supplier
// Service's admin-only endpoints (FR F2.2).
//
// INTERIM DESIGN: the team has not yet decided how identity/role is
// propagated between services (e.g. a JWT issued by the User Service vs.
// a header set by a gateway). Until that's settled, RoleExtractor reads a
// plain "X-User-Role" header. Swap the body of headerRoleExtractor (or
// provide a different RoleExtractor) once the real auth mechanism is
// agreed — no other code in this service needs to change.
package middleware

import "net/http"

const AdminRole = "ADMIN"

// RoleExtractor pulls the caller's role out of an incoming request.
type RoleExtractor func(r *http.Request) string

// HeaderRoleExtractor is the interim implementation: it trusts an
// "X-User-Role" header verbatim. This is NOT secure for production use —
// it exists only so the admin endpoints have a role check to develop and
// test against before the real auth mechanism is decided.
func HeaderRoleExtractor(r *http.Request) string {
	return r.Header.Get("X-User-Role")
}

// RequireAdmin rejects any request whose role (per extract) isn't ADMIN.
func RequireAdmin(extract RoleExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if extract(r) != AdminRole {
				http.Error(w, `{"error":"admin role required"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
