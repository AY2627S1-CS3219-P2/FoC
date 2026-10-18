//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: The e2e checks that order-service refuses an access token that has
//   been revoked, after a logout and after a suspension, through the whole
//   stack and the shared Redis (D-054, D-056).
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"net/http"
	"testing"
)

// TestLogoutRevokesTheAccessToken logs a student out and expects the access
// token they still hold to be refused by order-service, though it has not
// expired: user-service blocklisted it in the shared Redis (D-054, D-056).
func TestLogoutRevokesTheAccessToken(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u := s.newStudent(t)
	expectStatus(t, s.send(t, http.MethodGet, ordersPath+"/open", u.token, nil), http.StatusOK)

	s.logout(t, u)

	expectStatus(t, s.send(t, http.MethodGet, ordersPath+"/open", u.token, nil), http.StatusUnauthorized)
}

// TestSuspensionRevokesTheAccessToken has an ADMIN suspend a student and
// expects the access token the student already holds to be refused by
// order-service: user-service wrote the suspension to the shared Redis
// (D-054, D-056). The student is the test's own, so the suspension touches
// no other test.
func TestSuspensionRevokesTheAccessToken(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u, admin := s.newStudent(t), s.admin(t)
	expectStatus(t, s.send(t, http.MethodGet, ordersPath+"/open", u.token, nil), http.StatusOK)

	s.suspend(t, admin, u)

	expectStatus(t, s.send(t, http.MethodGet, ordersPath+"/open", u.token, nil), http.StatusUnauthorized)
}
