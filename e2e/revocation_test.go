//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: The e2e checks that order-service refuses an access token that has
//   been revoked, after a logout and after a suspension, through the whole
//   stack and the shared Redis (D-054, D-056).
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
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

// TestGatewayRefusesALoggedOutToken logs a student out and expects the token
// they still hold to be refused on routes whose service does not check the
// revocation keys itself, supplier reads and the credit account, so only the
// gateway's check can refuse it there (D-054, D-077).
func TestGatewayRefusesALoggedOutToken(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u := s.newStudent(t)
	s.startingBalance(t, u)
	expectStatus(t, s.send(t, http.MethodGet, suppliersPath, u.token, nil), http.StatusOK)

	s.logout(t, u)

	for _, path := range []string{suppliersPath, creditsPath + "/accounts/me"} {
		expectStatus(t, s.send(t, http.MethodGet, path, u.token, nil), http.StatusUnauthorized)
	}
}

// TestGatewayRefusesASuspendedTokenUntilReactivated has an ADMIN suspend a
// student and expects the student's token to be refused on supplier reads,
// the credit account and the student's own profile. Once the ADMIN reactivates
// them, a fresh login gets through, while the old token, issued before the
// suspension, stays refused. The fresh login is polled for rather than slept
// on, because a token issued in the suspension's own second is refused too.
func TestGatewayRefusesASuspendedTokenUntilReactivated(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u, admin := s.newStudent(t), s.admin(t)
	s.startingBalance(t, u)

	s.suspend(t, admin, u)

	for _, path := range []string{suppliersPath, creditsPath + "/accounts/me", usersPath + "/" + u.id} {
		expectStatus(t, s.send(t, http.MethodGet, path, u.token, nil), http.StatusUnauthorized)
	}

	reactivate := s.send(t, http.MethodPatch, usersPath+"/"+u.id+"/status", admin.token, statusRequest{Status: "ACTIVE"})
	expectStatus(t, reactivate, http.StatusOK)
	eventually(t, 5*time.Second, "a fresh login after reactivation reaching GET "+suppliersPath,
		func(ctx context.Context) (bool, string) {
			fresh, err := s.login(ctx, u.username, studentPassword)
			if err != nil {
				return false, err.Error()
			}
			r, err := s.request(ctx, http.MethodGet, suppliersPath, fresh.token, nil, nil)
			if err != nil {
				return false, err.Error()
			}
			return r.status == http.StatusOK, r.String()
		})
	expectStatus(t, s.send(t, http.MethodGet, suppliersPath, u.token, nil), http.StatusUnauthorized)
}
