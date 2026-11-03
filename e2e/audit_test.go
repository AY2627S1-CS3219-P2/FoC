//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The e2e checks of the admin audit log through the whole stack (D-104
//   to D-115, D-124): each service's admin actions leave a row, a refused
//   attempt is logged as refused, and only an admin reads the log.
// Author review: PENDING — <reviewer to complete>

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The log's gateway paths (D-124 §3).
const (
	supplierAuditPath = suppliersPath + "/audit-log"
	userAuditPath     = usersPath + "/audit-log"
	orderAuditPath    = ordersPath + "/audit-log"
)

// auditRow is the part of a log row the suite checks. user-service and
// supplier-service answer in snake_case and order-service in camelCase, so
// there are two shapes, and both read into this one.
type auditRow struct {
	ActorID       string
	Action        string
	TargetID      string
	Outcome       string
	RefusalReason string
	CorrelationID string
}

type snakeAuditPage struct {
	Items []struct {
		ActorID       string `json:"actor_id"`
		Action        string `json:"action"`
		TargetID      string `json:"target_id"`
		Outcome       string `json:"outcome"`
		RefusalReason string `json:"refusal_reason"`
		CorrelationID string `json:"correlation_id"`
	} `json:"items"`
	Total int `json:"total"`
}

type camelAuditPage struct {
	Items []struct {
		ActorID       string `json:"actorId"`
		Action        string `json:"action"`
		TargetID      string `json:"targetId"`
		Outcome       string `json:"outcome"`
		RefusalReason string `json:"refusalReason"`
		CorrelationID string `json:"correlationId"`
	} `json:"items"`
	Total int `json:"total"`
}

// auditLog reads the log at path for one target as u, newest first. camel is
// true for order-service's.
func (s *stack) auditLog(t *testing.T, u user, path, targetID string, camel bool) []auditRow {
	t.Helper()
	r := s.send(t, http.MethodGet, path+"?target_id="+url.QueryEscape(targetID), u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var rows []auditRow
	if camel {
		var page camelAuditPage
		require.NoError(t, r.decode(&page))
		require.Equal(t, len(page.Items), page.Total, "one target's rows fit a page")
		for _, i := range page.Items {
			rows = append(rows, auditRow{i.ActorID, i.Action, i.TargetID, i.Outcome, i.RefusalReason, i.CorrelationID})
		}
		return rows
	}
	var page snakeAuditPage
	require.NoError(t, r.decode(&page))
	require.Equal(t, len(page.Items), page.Total, "one target's rows fit a page")
	for _, i := range page.Items {
		rows = append(rows, auditRow{i.ActorID, i.Action, i.TargetID, i.Outcome, i.RefusalReason, i.CorrelationID})
	}
	return rows
}

func actionsAndOutcomes(rows []auditRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Action+":"+r.Outcome)
	}
	return out
}

// TestSupplierWritesAreAudited has an ADMIN create, edit and delete a supplier
// and expects a row for each, newest first, naming the admin from the token
// and carrying a correlation id (D-107, D-108).
func TestSupplierWritesAreAudited(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin := s.admin(t)
	body := testSupplier()
	created := s.createSupplier(t, admin, body)
	body.Description = "changed by the audit test"
	expectStatus(t, s.send(t, http.MethodPut, suppliersPath+"/"+created.ID, admin.token, body), http.StatusOK)
	expectStatus(t, s.send(t, http.MethodDelete, suppliersPath+"/"+created.ID, admin.token, nil), http.StatusNoContent)

	rows := s.auditLog(t, admin, supplierAuditPath, created.ID, false)

	assert.Equal(t, []string{"supplier.delete:succeeded", "supplier.update:succeeded", "supplier.create:succeeded"}, actionsAndOutcomes(rows))
	for _, row := range rows {
		assert.Equal(t, admin.id, row.ActorID, "the actor is the token's subject")
		assert.Equal(t, created.ID, row.TargetID)
		assert.NotEmpty(t, row.CorrelationID)
	}
}

// TestRefusedSupplierDeleteIsAudited expects a delete the unfinished-errand
// rule refuses (409) to be logged as refused, with the reason (D-109).
func TestRefusedSupplierDeleteIsAudited(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin, requester := s.admin(t), s.newStudent(t)
	created := s.createSupplier(t, admin, testSupplier())
	s.startingBalance(t, requester) // waits for the requester's credit account
	in := s.errandInput(t, requester, 5)
	in.SupplierID = created.ID
	e := s.createErrand(t, requester, in).Errand
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		_, _ = s.request(ctx, http.MethodPost, actionPath(e.ID, "cancel"), requester.token, nil, nil)
	})

	expectStatus(t, s.send(t, http.MethodDelete, suppliersPath+"/"+created.ID, admin.token, nil), http.StatusConflict)

	rows := s.auditLog(t, admin, supplierAuditPath, created.ID, false)
	assert.Equal(t, []string{"supplier.delete:refused", "supplier.create:succeeded"}, actionsAndOutcomes(rows))
	assert.NotEmpty(t, rows[0].RefusalReason)
}

// TestAccountStatusChangesAreAudited has an ADMIN suspend and reinstate a
// student, and try to suspend themself, which is refused with 403 and logged
// (D-109, D-119). The admin stays active: the refusal happens before any write.
func TestAccountStatusChangesAreAudited(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin, student := s.admin(t), s.newStudent(t)
	s.suspend(t, admin, student)
	r := s.send(t, http.MethodPatch, usersPath+"/"+student.id+"/status", admin.token, statusRequest{Status: "ACTIVE"})
	expectStatus(t, r, http.StatusOK)

	rows := s.auditLog(t, admin, userAuditPath, student.id, false)
	assert.Equal(t, []string{"account.reactivate:succeeded", "account.suspend:succeeded"}, actionsAndOutcomes(rows))
	for _, row := range rows {
		assert.Equal(t, admin.id, row.ActorID)
		assert.Equal(t, student.id, row.TargetID)
	}

	r = s.send(t, http.MethodPatch, usersPath+"/"+admin.id+"/status", admin.token, statusRequest{Status: "SUSPENDED"})
	expectStatus(t, r, http.StatusForbidden)
	own := s.auditLog(t, admin, userAuditPath, admin.id, false)
	require.NotEmpty(t, own)
	assert.Equal(t, "account.suspend", own[0].Action)
	assert.Equal(t, "refused", own[0].Outcome)
	assert.NotEmpty(t, own[0].RefusalReason)
	expectStatus(t, s.send(t, http.MethodGet, usersPath+"/"+admin.id, admin.token, nil), http.StatusOK)
}

// TestAdminErrandCancelIsAudited expects an admin's cancel of an accepted
// errand to leave an errand.cancel row that records the state change (D-113).
func TestAdminErrandCancelIsAudited(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier, admin := s.newStudent(t), s.newStudent(t), s.admin(t)
	s.startingBalance(t, requester) // waits for the requester's credit account
	e := s.createErrand(t, requester, s.errandInput(t, requester, 6)).Errand
	expectStatus(t, s.accept(t, courier, e), http.StatusOK)
	detailOf(t, s.act(t, admin, e.ID, "cancel"), http.StatusOK)

	rows := s.auditLog(t, admin, orderAuditPath, e.ID, true)

	assert.Equal(t, []string{"errand.cancel:succeeded"}, actionsAndOutcomes(rows))
	assert.Equal(t, admin.id, rows[0].ActorID)
	assert.NotEmpty(t, rows[0].CorrelationID)
}

// TestOnlyAnAdminReadsTheAuditLog expects each service's log to answer a
// student 403 and a request with no token 401 (D-110).
func TestOnlyAnAdminReadsTheAuditLog(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student := s.newStudent(t)
	for _, path := range []string{supplierAuditPath, userAuditPath, orderAuditPath} {
		expectStatus(t, s.send(t, http.MethodGet, path, student.token, nil), http.StatusForbidden)
		expectStatus(t, s.send(t, http.MethodGet, path, "", nil), http.StatusUnauthorized)
	}
}
