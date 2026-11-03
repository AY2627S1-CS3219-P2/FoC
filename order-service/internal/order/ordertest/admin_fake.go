// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The in-memory Repository's admin dashboard methods (D-104 to D-117):
//   the audit log, the admin errand list and a user's errand counts.
// Author review: PENDING — <reviewer to complete>

package ordertest

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
)

// Audits returns every audit row saved so far, oldest first.
func (r *Repository) Audits() []*order.AuditEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.audits)
}

// RecordAudit implements order.Repository.
func (r *Repository) RecordAudit(_ context.Context, e *order.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("RecordAudit"); err != nil {
		return err
	}
	r.audits = append(r.audits, e)
	return nil
}

// ListAudit implements order.Repository: newest first, filtered, paged.
func (r *Repository) ListAudit(_ context.Context, f order.AuditFilter) (*order.AuditPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*order.AuditEntry
	for _, e := range r.audits {
		switch {
		case f.Action != "" && e.Action != f.Action,
			f.Outcome != "" && e.Outcome != f.Outcome,
			f.ActorID != "" && !strings.EqualFold(e.ActorID, f.ActorID),
			f.TargetID != "" && !strings.EqualFold(e.TargetID, f.TargetID),
			f.From != nil && e.OccurredAt.Before(*f.From),
			f.To != nil && !e.OccurredAt.Before(*f.To):
			continue
		}
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(a, b *order.AuditEntry) int { return b.OccurredAt.Compare(a.OccurredAt) })
	total := len(out)
	out = page(out, f.Limit, f.Offset)
	if out == nil {
		out = []*order.AuditEntry{}
	}
	return &order.AuditPage{Items: out, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// PurgeAudit implements order.Repository: it removes rows older than 6 months.
func (r *Repository) PurgeAudit(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("PurgeAudit"); err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().AddDate(0, -6, 0)
	kept := r.audits[:0]
	var removed int64
	for _, e := range r.audits {
		if e.OccurredAt.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	r.audits = kept
	return removed, nil
}

// ListAdminErrands implements order.Repository.
func (r *Repository) ListAdminErrands(_ context.Context, f order.AdminErrandFilter) (order.AdminErrandPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.filter(func(o order.Order) bool {
		switch {
		case f.State != nil && o.State != *f.State,
			f.State == nil && f.Scope == order.ScopeHistory && !o.State.Final(),
			f.State == nil && f.Scope == order.ScopeActive && o.State.Final(),
			f.From != nil && o.StateChangedAt.Before(*f.From),
			f.To != nil && !o.StateChangedAt.Before(*f.To),
			f.SupplierID != nil && o.Supplier.ID != *f.SupplierID,
			f.UserID != nil && o.RequesterID != *f.UserID && (o.CourierID == nil || *o.CourierID != *f.UserID):
			return false
		}
		return true
	})
	switch f.Sort {
	case order.SortSupplierName:
		slices.SortStableFunc(out, func(a, b order.Order) int {
			if c := strings.Compare(strings.ToLower(a.Supplier.Name), strings.ToLower(b.Supplier.Name)); c != 0 {
				return c
			}
			return b.StateChangedAt.Compare(a.StateChangedAt)
		})
	case order.SortOldest:
		slices.SortStableFunc(out, func(a, b order.Order) int { return a.StateChangedAt.Compare(b.StateChangedAt) })
	default:
		slices.SortStableFunc(out, func(a, b order.Order) int { return b.StateChangedAt.Compare(a.StateChangedAt) })
	}
	total := len(out)
	items := page(out, f.Limit, f.Offset)
	if items == nil {
		items = []order.Order{}
	}
	return order.AdminErrandPage{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// CountByUser implements order.Repository.
func (r *Repository) CountByUser(_ context.Context, userID uuid.UUID) (order.ErrandCounts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := order.ErrandCounts{Requested: map[order.State]int{}, Assigned: map[order.State]int{}}
	for _, o := range r.orders {
		if o.RequesterID == userID {
			counts.Requested[o.State]++
		}
		if o.CourierID != nil && *o.CourierID == userID {
			counts.Assigned[o.State]++
		}
	}
	return counts, nil
}

// page returns the window of s that offset and limit name, or nil past its end.
func page[T any](s []T, limit, offset int) []T {
	if offset >= len(s) {
		return nil
	}
	s = s[offset:]
	if limit > 0 && limit < len(s) {
		s = s[:limit]
	}
	return s
}
