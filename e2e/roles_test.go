//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-10
// Scope: Whole-stack coverage for promoting and demoting an administrator
//   through the gateway (user-service's PATCH /api/v1/users/{uid}/role): the
//   role change, the signed-out token it causes, its refusals, and its audit
//   rows. It never touches the shared admin's role, so the parallel tests that
//   sign in as that admin are not disturbed.
// Author review: PENDING — <reviewer to complete>

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roleRequest struct {
	Role string `json:"role"`
}

// setRole has actor ask for target's role to become role.
func (s *stack) setRole(t *testing.T, actor, target user, role string) response {
	t.Helper()
	return s.send(t, http.MethodPatch, usersPath+"/"+target.id+"/role", actor.token, roleRequest{Role: role})
}

// signInAgain logs u in until the new token is accepted. A role change signs
// the target out by a cutoff in whole seconds, as a token's iat is (user-service
// tokenCutoff), so a login in the same second as the change is refused once and
// works the next; the browser's user signs in again by hand, seconds later.
func (s *stack) signInAgain(t *testing.T, u user) user {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var again user
	last := ""
	_, ok := poll(ctx, func(ctx context.Context) (bool, string) {
		var err error
		again, err = s.login(ctx, u.username, studentPassword)
		if err != nil {
			last = err.Error()
			return false, last
		}
		r, err := s.request(ctx, http.MethodGet, usersPath+"/"+again.id, again.token, nil, nil)
		if err != nil || r.status != http.StatusOK {
			last = "the new token was not accepted"
			return false, last
		}
		return true, ""
	})
	require.True(t, ok, last)
	return again
}

// TestAdminPromotesAndDemotesAStudent follows one account from STUDENT to
// ADMIN and back: each change ends the account's tokens, the new token carries
// the new role, a new admin cannot demote themselves, and both changes and the
// refusal are in the audit log.
func TestAdminPromotesAndDemotesAStudent(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student, admin := s.newStudent(t), s.admin(t)
	require.Equal(t, "STUDENT", student.role)
	expectStatus(t, s.send(t, http.MethodGet, usersPath, student.token, nil), http.StatusForbidden)

	expectStatus(t, s.setRole(t, admin, student, "ADMIN"), http.StatusNoContent)
	expectStatus(t, s.send(t, http.MethodGet, usersPath+"/"+student.id, student.token, nil), http.StatusUnauthorized)

	promoted := s.signInAgain(t, student)
	assert.Equal(t, "ADMIN", promoted.role)
	expectStatus(t, s.send(t, http.MethodGet, usersPath, promoted.token, nil), http.StatusOK)

	expectStatus(t, s.setRole(t, promoted, promoted, "STUDENT"), http.StatusForbidden)
	expectStatus(t, s.send(t, http.MethodGet, usersPath, promoted.token, nil), http.StatusOK)

	expectStatus(t, s.setRole(t, admin, promoted, "STUDENT"), http.StatusNoContent)
	expectStatus(t, s.send(t, http.MethodGet, usersPath, promoted.token, nil), http.StatusUnauthorized)

	demoted := s.signInAgain(t, student)
	assert.Equal(t, "STUDENT", demoted.role)
	expectStatus(t, s.send(t, http.MethodGet, usersPath, demoted.token, nil), http.StatusForbidden)

	rows := s.auditLog(t, admin, userAuditPath, student.id, false)
	var succeeded, refused int
	for _, row := range rows {
		require.Equal(t, "account.change_role", row.Action)
		switch row.Outcome {
		case "succeeded":
			succeeded++
			assert.Equal(t, admin.id, row.ActorID)
		case "refused":
			refused++
			assert.Equal(t, student.id, row.ActorID)
			assert.NotEmpty(t, row.RefusalReason)
		}
	}
	assert.Equal(t, 2, succeeded, "one row for the promotion and one for the demotion: %v", actionsAndOutcomes(rows))
	assert.Equal(t, 1, refused, "one row for the refused self-demotion: %v", actionsAndOutcomes(rows))
}

// TestRoleChangeRefusals expects only an ADMIN to change a role, only an ACTIVE
// student to be promoted, and a bad request to be refused before anything
// changes.
func TestRoleChangeRefusals(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student, other, suspended, admin := s.newStudent(t), s.newStudent(t), s.newStudent(t), s.admin(t)
	s.suspend(t, admin, suspended)

	expectStatus(t, s.setRole(t, student, other, "ADMIN"), http.StatusForbidden)
	expectStatus(t, s.send(t, http.MethodPatch, usersPath+"/"+other.id+"/role", "", roleRequest{Role: "ADMIN"}), http.StatusUnauthorized)

	expectStatus(t, s.setRole(t, admin, suspended, "ADMIN"), http.StatusConflict)
	expectStatus(t, s.setRole(t, admin, other, "SUPERUSER"), http.StatusBadRequest)
	expectStatus(t, s.setRole(t, admin, user{id: randomUUID()}, "ADMIN"), http.StatusNotFound)
	expectStatus(t, s.send(t, http.MethodPatch, usersPath+"/not-a-uuid/role", admin.token, roleRequest{Role: "ADMIN"}), http.StatusBadRequest)

	// Every refusal above left the accounts as they were.
	expectStatus(t, s.send(t, http.MethodGet, usersPath+"/"+other.id, other.token, nil), http.StatusOK)
	expectStatus(t, s.send(t, http.MethodGet, usersPath, other.token, nil), http.StatusForbidden)
}
