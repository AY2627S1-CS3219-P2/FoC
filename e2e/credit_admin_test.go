//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The e2e checks of credit-service's admin routes through the gateway
//   (D-150 to D-153): only an admin reaches them, and a Re-verify of a wallet
//   that is not flagged changes nothing and leaves no audit row. Flagging a
//   wallet needs a corrupted balance, which the gateway cannot make, so the
//   clear and the still-flagged paths are covered by credit-service's
//   integration tests.
// Author review: PENDING — <reviewer to complete>

package e2e

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The credit admin routes' gateway paths (D-151).
const (
	creditAdminPath      = creditsPath + "/admin"
	flaggedAccountsPath  = creditAdminPath + "/flagged-accounts"
	creditAuditLogPath   = creditAdminPath + "/audit-log"
	adminWalletPathOf    = "/account"
	adminReconcilePathOf = "/reconcile"
)

func adminUserPath(userID, tail string) string {
	return creditAdminPath + "/users/" + userID + tail
}

// TestCreditAdminRoutesAreForAdminsOnly expects each route to answer a student
// 403 and a request with no token 401 (D-151).
func TestCreditAdminRoutesAreForAdminsOnly(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student := s.newStudent(t)
	routes := []struct{ method, path string }{
		{http.MethodGet, flaggedAccountsPath},
		{http.MethodGet, adminUserPath(student.id, adminWalletPathOf)},
		{http.MethodPost, adminUserPath(student.id, adminReconcilePathOf)},
		{http.MethodGet, creditAuditLogPath},
	}
	for _, route := range routes {
		expectStatus(t, s.send(t, route.method, route.path, student.token, nil), http.StatusForbidden)
		expectStatus(t, s.send(t, route.method, route.path, "", nil), http.StatusUnauthorized)
	}
}

// TestReverifyOfAHealthyWalletChangesNothing has an ADMIN read a new student's
// wallet and Re-verify it. It is not flagged, so nothing changes, the wallet is
// not on the flagged list, and no audit row is written (D-150, D-152).
func TestReverifyOfAHealthyWalletChangesNothing(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin, student := s.admin(t), s.newStudent(t)
	start := s.startingBalance(t, student)

	// The wallet row is made by the user.registered event; the user-facing read
	// invents a starting wallet until then, so wait on the admin read, which
	// does not.
	var wallet struct {
		AvailableCredits int64  `json:"availableCredits"`
		ReservedCredits  int64  `json:"reservedCredits"`
		IntegrityStatus  string `json:"integrityStatus"`
	}
	eventually(t, eventDeadline, "an admin read of the new wallet", func(ctx context.Context) (bool, string) {
		r, err := s.request(ctx, http.MethodGet, adminUserPath(student.id, adminWalletPathOf), admin.token, nil, nil)
		if err != nil {
			return false, err.Error()
		}
		if r.status != http.StatusOK {
			return false, "status " + http.StatusText(r.status)
		}
		return r.decode(&wallet) == nil, "decoded"
	})
	assert.Equal(t, "CONSISTENT", wallet.IntegrityStatus)
	assert.EqualValues(t, start.available, wallet.AvailableCredits)
	assert.EqualValues(t, start.reserved, wallet.ReservedCredits)

	r := s.send(t, http.MethodPost, adminUserPath(student.id, adminReconcilePathOf), admin.token, nil)
	expectStatus(t, r, http.StatusOK)
	var result struct {
		Outcome string `json:"outcome"`
	}
	require.NoError(t, r.decode(&result))
	assert.Equal(t, "NOT_FLAGGED", result.Outcome)

	var flagged struct {
		Items []struct {
			UserID string `json:"userId"`
		} `json:"items"`
	}
	r = s.send(t, http.MethodGet, flaggedAccountsPath, admin.token, nil)
	expectStatus(t, r, http.StatusOK)
	require.NoError(t, r.decode(&flagged))
	for _, item := range flagged.Items {
		assert.NotEqual(t, student.id, item.UserID, "a healthy wallet is not on the flagged list")
	}

	rows := s.auditLog(t, admin, creditAuditLogPath, student.id, true)
	assert.Empty(t, rows, "a Re-verify that changes nothing is not logged")
}

// TestCreditAdminReadsAWalletThatDoesNotExistAs404 expects an admin's read of a
// user with no credit account to answer 404, not a synthesized starting wallet.
func TestCreditAdminReadsAWalletThatDoesNotExistAs404(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin := s.admin(t)
	const unknown = "00000000-0000-4000-8000-000000000001"
	expectStatus(t, s.send(t, http.MethodGet, adminUserPath(unknown, adminWalletPathOf), admin.token, nil), http.StatusNotFound)
	expectStatus(t, s.send(t, http.MethodPost, adminUserPath(unknown, adminReconcilePathOf), admin.token, nil), http.StatusNotFound)
}
