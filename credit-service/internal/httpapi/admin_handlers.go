// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: credit-service's admin routes (D-150 to D-153): the flagged-accounts
//   list, an account's wallet, Re-verify and the audit log. Each checks for the
//   ADMIN role itself (D-151), so a student's token is refused with 403 even if
//   the gateway were bypassed.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/gen"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// adminRole is the role claim of an admin's access token.
const adminRole = "ADMIN"

// requireAdmin returns the verified caller when it is an ADMIN. Otherwise it
// has already answered 401 (no identity) or 403, and ok is false.
func requireAdmin(w http.ResponseWriter, r *http.Request) (Identity, bool) {
	identity, ok := IdentityFromContext(r.Context())
	if !ok || identity.UserID == uuid.Nil {
		writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "missing caller identity")
		return Identity{}, false
	}
	if identity.Role != adminRole {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "admin access required")
		return Identity{}, false
	}
	return identity, true
}

// ListFlaggedAccounts handles GET /admin/flagged-accounts (D-150).
func (h *Handler) ListFlaggedAccounts(w http.ResponseWriter, r *http.Request, params gen.ListFlaggedAccountsParams) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	var limit, offset int
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	page, err := h.creditService.ListFlaggedAccounts(r.Context(), limit, offset)
	if err != nil {
		h.mapDomainError(w, r, err)
		return
	}
	items := make([]gen.FlaggedAccount, 0, len(page.Items))
	for _, a := range page.Items {
		items = append(items, gen.FlaggedAccount{
			UserId:     a.UserID,
			ReasonCode: a.ReasonCode,
			Detail:     a.Detail,
			Expected:   a.Expected,
			Observed:   a.Observed,
			FlaggedAt:  a.FlaggedAt,
		})
	}
	writeJSON(w, http.StatusOK, gen.FlaggedAccountsPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset})
}

// GetAccountForAdmin handles GET /admin/users/{userId}/account (D-150): the
// stored account with its real balances, flagged or not.
func (h *Handler) GetAccountForAdmin(w http.ResponseWriter, r *http.Request, userId openapi_types.UUID) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	a, err := h.creditService.GetAdminAccount(r.Context(), userId)
	if err != nil {
		h.mapAdminError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.AdminAccountResponse{
		UserId:           a.UserID,
		AvailableCredits: a.AvailableCredits,
		ReservedCredits:  a.ReservedCredits,
		IntegrityStatus:  gen.IntegrityStatus(a.IntegrityStatus),
		FlaggedAt:        a.IntegrityFlaggedAt,
		ReasonCode:       a.IntegrityReasonCode,
		Detail:           a.IntegrityDetail,
		Expected:         a.IntegrityExpected,
		Observed:         a.IntegrityObserved,
		CreatedAt:        a.CreatedAt,
		UpdatedAt:        a.UpdatedAt,
	})
}

// ReverifyAccount handles POST /admin/users/{userId}/reconcile (D-150, D-153).
func (h *Handler) ReverifyAccount(w http.ResponseWriter, r *http.Request, userId openapi_types.UUID) {
	identity, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	result, err := h.creditService.ReverifyAccount(r.Context(), credit.AuditContext{
		ActorID:       identity.UserID.String(),
		ActorRole:     identity.Role,
		CorrelationID: middleware.GetReqID(r.Context()),
	}, userId)
	if err != nil {
		h.mapAdminError(w, r, err)
		return
	}
	resp := gen.ReverifyResponse{Outcome: gen.ReverifyOutcome(result.Outcome)}
	if f := result.Finding; f != nil {
		resp.ReasonCode = &f.ReasonCode
		resp.Detail = &f.Detail
		resp.Expected = f.Expected
		resp.Observed = f.Observed
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListAuditLog handles GET /admin/audit-log (D-110, D-152): one page of the
// admin audit log, newest first.
func (h *Handler) ListAuditLog(w http.ResponseWriter, r *http.Request, params gen.ListAuditLogParams) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	f := credit.AuditFilter{}
	if params.Action != nil {
		f.Action = string(*params.Action)
	}
	if params.Outcome != nil {
		f.Outcome = string(*params.Outcome)
	}
	if params.ActorId != nil {
		f.ActorID = *params.ActorId
	}
	if params.TargetId != nil {
		f.TargetID = *params.TargetId
	}
	var err error
	if f.From, err = parseAdminTime(params.From, "from"); err != nil {
		writeError(w, http.StatusBadRequest, gen.INVALIDREQUEST, err.Error())
		return
	}
	if f.To, err = parseAdminTime(params.To, "to"); err != nil {
		writeError(w, http.StatusBadRequest, gen.INVALIDREQUEST, err.Error())
		return
	}
	if params.Limit != nil {
		f.Limit = *params.Limit
	}
	if params.Offset != nil {
		f.Offset = *params.Offset
	}
	page, err := h.creditService.ListAuditLog(r.Context(), f)
	if err != nil {
		h.mapDomainError(w, r, err)
		return
	}
	items := make([]gen.AuditEntry, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, toAuditEntry(e))
	}
	writeJSON(w, http.StatusOK, gen.AuditLogPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset})
}

// mapAdminError answers an admin route's error. A user with no account is 404
// here, where the user-facing routes say the wallet is not ready (409).
func (h *Handler) mapAdminError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, credit.ErrWalletNotReady) {
		writeError(w, http.StatusNotFound, gen.WALLETNOTREADY, "the user has no credit account")
		return
	}
	h.mapDomainError(w, r, err)
}

func toAuditEntry(e *credit.AuditEntry) gen.AuditEntry {
	out := gen.AuditEntry{
		Action:        e.Action,
		ActorRole:     e.ActorRole,
		CorrelationId: e.CorrelationID,
		OccurredAt:    e.OccurredAt,
		Outcome:       gen.AuditEntryOutcome(e.Outcome),
		Reason:        e.Reason,
		RefusalReason: e.RefusalReason,
		TargetId:      e.TargetID,
		TargetType:    e.TargetType,
	}
	// The ids are uuids in the table; a value that is not one is shown as the nil id.
	out.Id, _ = parseUUID(e.ID)
	out.ActorId, _ = parseUUID(e.ActorID)
	if e.BeforeState != nil {
		out.BeforeState = &e.BeforeState
	}
	if e.AfterState != nil {
		out.AfterState = &e.AfterState
	}
	if e.AdditionalComments != nil {
		out.AdditionalComments = &e.AdditionalComments
	}
	return out
}

// parseAdminTime parses an optional time: RFC 3339, or a bare date read as
// midnight UTC. nil is nil.
func parseAdminTime(v *string, name string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(*v)); err == nil {
			t = t.UTC()
			return &t, nil
		}
	}
	return nil, fmt.Errorf("%s must be an RFC 3339 time or YYYY-MM-DD", name)
}

// parseUUID reads a uuid, returning the zero value and false for anything else.
func parseUUID(s string) (openapi_types.UUID, bool) {
	id, err := uuid.Parse(s)
	return id, err == nil
}
