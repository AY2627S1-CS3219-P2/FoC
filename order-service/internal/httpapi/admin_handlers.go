// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin dashboard's read endpoints on order-service (D-104 to D-117):
//   the filter of the admin errand list, the audit log and a user's errand
//   counts. All are admin-only; the service decides that from the token's role.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

// adminListOptionsGiven reports whether a list request names any filter, sort
// or paging option beyond scope, which turns it into the paged admin list.
func adminListOptionsGiven(p gen.ListActiveErrandsParams) bool {
	return p.State != nil || p.From != nil || p.To != nil || p.SupplierId != nil ||
		p.UserId != nil || p.Sort != nil || p.Limit != nil || p.Offset != nil
}

// adminFilter turns the list's query parameters into the domain filter. A time
// that does not parse is an error for the caller's 400.
func adminFilter(p gen.ListActiveErrandsParams) (order.AdminErrandFilter, error) {
	f := order.AdminErrandFilter{Scope: order.Scope(p.Scope)}
	if p.State != nil {
		st := order.State(*p.State)
		f.State = &st
	}
	var err error
	if f.From, err = parseAdminTime(p.From, "from"); err != nil {
		return f, err
	}
	if f.To, err = parseAdminTime(p.To, "to"); err != nil {
		return f, err
	}
	if p.SupplierId != nil {
		id := uuid.UUID(*p.SupplierId)
		f.SupplierID = &id
	}
	if p.UserId != nil {
		id := uuid.UUID(*p.UserId)
		f.UserID = &id
	}
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	if p.Limit != nil {
		f.Limit = *p.Limit
	}
	if p.Offset != nil {
		f.Offset = *p.Offset
	}
	return f, nil
}

// parseAdminTime parses an optional time: RFC 3339, or a bare date read as
// midnight UTC. nil is nil.
func parseAdminTime(v *string, name string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, *v); err == nil {
			t = t.UTC()
			return &t, nil
		}
	}
	return nil, fmt.Errorf("%s must be an RFC 3339 time or YYYY-MM-DD", name)
}

// ListAuditLog handles GET /audit-log, admin-only (D-110): one page of the admin
// audit log, newest first.
func (s *server) ListAuditLog(w http.ResponseWriter, r *http.Request, params gen.ListAuditLogParams) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	f := order.AuditFilter{}
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if f.To, err = parseAdminTime(params.To, "to"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if params.Limit != nil {
		f.Limit = *params.Limit
	}
	if params.Offset != nil {
		f.Offset = *params.Offset
	}
	page, err := s.svc.AuditLog(r.Context(), c, f)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	out := gen.AuditLogPage{Items: make([]gen.AuditEntry, 0, len(page.Items)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, e := range page.Items {
		actor, _ := uuid.Parse(e.ActorID)
		id, _ := uuid.Parse(e.ID)
		entry := gen.AuditEntry{
			Id: id, OccurredAt: e.OccurredAt, ActorId: actor, ActorRole: e.ActorRole, Action: e.Action,
			TargetType: e.TargetType, TargetId: e.TargetID, Outcome: gen.AuditEntryOutcome(e.Outcome),
			RefusalReason: e.RefusalReason, Reason: e.Reason, CorrelationId: e.CorrelationID,
		}
		if e.BeforeState != nil {
			entry.BeforeState = &e.BeforeState
		}
		if e.AfterState != nil {
			entry.AfterState = &e.AfterState
		}
		if e.AdditionalComments != nil {
			entry.AdditionalComments = &e.AdditionalComments
		}
		out.Items = append(out.Items, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// GetUserErrandCounts handles GET /users/{userId}/errand-counts, admin-only
// (D-117): how many errands a user has in each state, as requester and as the
// courier now assigned.
func (s *server) GetUserErrandCounts(w http.ResponseWriter, r *http.Request, userID openapi_types.UUID) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	counts, err := s.svc.ErrandCounts(r.Context(), c, uuid.UUID(userID))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.ErrandCounts{Requested: stateCounts(counts.Requested), Assigned: stateCounts(counts.Assigned)})
}

func stateCounts(in map[order.State]int) map[string]int {
	out := make(map[string]int, len(in))
	for st, n := range in {
		out[string(st)] = n
	}
	return out
}
