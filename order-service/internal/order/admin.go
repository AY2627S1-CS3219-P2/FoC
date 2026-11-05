// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin side of Service (D-104 to D-117): the filtered, paged past
//   and active errand lists, a user's errand counts, the audit log's read, the
//   log of a refused or failed attempt, and the 6-month purge.
// Author review: PENDING — <reviewer to complete>

package order

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// PurgeInterval is how often the scheduled purge runs (D-115).
const PurgeInterval = 24 * time.Hour

// ListAdminErrands is the admin's errand list (D-105, D-117): every errand that
// is not final (scope active) or the completed, cancelled and expired ones
// (scope history), filtered, sorted and paged by offset, at most 50 a page.
func (s *Service) ListAdminErrands(ctx context.Context, c Caller, f AdminErrandFilter) (AdminErrandPage, error) {
	if c.Role != RoleAdmin {
		return AdminErrandPage{}, forbidden("only an admin can list every errand")
	}
	if f.Limit <= 0 || f.Limit > MaxAdminPage {
		f.Limit = MaxAdminPage
	}
	if f.Offset < 0 {
		return AdminErrandPage{}, invalid("offset must not be negative")
	}
	switch f.Scope {
	case ScopeActive, ScopeHistory:
	default:
		return AdminErrandPage{}, invalid("scope must be active or history")
	}
	if f.State != nil {
		if f.Scope == ScopeHistory && !f.State.Final() {
			return AdminErrandPage{}, invalid("state must be COMPLETED, CANCELLED or EXPIRED for history")
		}
		if f.Scope == ScopeActive && f.State.Final() {
			return AdminErrandPage{}, invalid("state must not be final for active")
		}
	}
	switch f.Sort {
	case "":
		f.Sort = SortRecent
		if f.Scope == ScopeActive {
			f.Sort = SortOldest
		}
	case SortRecent, SortSupplierName, SortOldest:
	default:
		return AdminErrandPage{}, invalid("sort must be recent, supplier_name or oldest")
	}
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return AdminErrandPage{}, invalid("from must be before to")
	}
	page, err := s.repo.ListAdminErrands(ctx, f)
	if err != nil {
		return AdminErrandPage{}, fmt.Errorf("list errands for admin: %w", err)
	}
	return page, nil
}

// ErrandCounts counts a user's errands by state for the admin's account review
// (D-117).
func (s *Service) ErrandCounts(ctx context.Context, c Caller, userID uuid.UUID) (ErrandCounts, error) {
	if c.Role != RoleAdmin {
		return ErrandCounts{}, forbidden("only an admin can count another user's errands")
	}
	counts, err := s.repo.CountByUser(ctx, userID)
	if err != nil {
		return ErrandCounts{}, fmt.Errorf("count errands of user %s: %w", userID, err)
	}
	return counts, nil
}

// AuditLog returns one page of the admin audit log, newest first (D-110).
func (s *Service) AuditLog(ctx context.Context, c Caller, f AuditFilter) (*AuditPage, error) {
	if c.Role != RoleAdmin {
		return nil, forbidden("only an admin can read the audit log")
	}
	if f.Limit <= 0 || f.Limit > MaxAdminPage {
		f.Limit = MaxAdminPage
	}
	if f.Offset < 0 {
		return nil, invalid("offset must not be negative")
	}
	switch f.Outcome {
	case "", OutcomeSucceeded, OutcomeRefused, OutcomeFailed:
	default:
		return nil, invalid("outcome must be succeeded, refused or failed")
	}
	page, err := s.repo.ListAudit(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	return page, nil
}

// recordAdminAttempt writes the audit row for an admin's cancel or complete that
// did not commit (D-109). It cannot share the action's transaction, which rolled
// back or never began, so it is a write of its own; if that fails too the
// attempt is logged and the caller still gets the real error. Only an admin's
// attempts are logged: a requester's refused cancel is not an admin action. The
// write ignores the request's cancellation, so a client that hangs up does not
// lose the row.
func (s *Service) recordAdminAttempt(ctx context.Context, c Caller, action string, id uuid.UUID, correlationID string, cause error) {
	if c.Role != RoleAdmin {
		return
	}
	outcome, reason := classify(cause)
	e := &AuditEntry{
		OccurredAt:    s.clock.Now(),
		ActorID:       c.UserID.String(),
		ActorRole:     string(c.Role),
		Action:        action,
		TargetType:    AuditTargetType,
		TargetID:      id.String(),
		Outcome:       outcome,
		RefusalReason: reason,
		CorrelationID: correlationID,
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repo.RecordAudit(ctx, e); err != nil && s.log != nil {
		s.log.Error("audit: could not record an admin attempt", slog.String("action", action),
			slog.String("outcome", outcome), slog.String("errand", id.String()), slog.String("actor", e.ActorID), slog.Any("error", err))
	}
}

// RunAuditPurge removes the entries older than 6 months once now and then every
// interval, until ctx ends (D-115). The database refuses to delete anything
// newer (D-114). A failed run is logged and the next one tries again.
func (s *Service) RunAuditPurge(ctx context.Context, interval time.Duration) {
	run := func() {
		n, err := s.repo.PurgeAudit(ctx)
		if err != nil {
			if s.log != nil {
				s.log.Error("audit: purge failed", slog.Any("error", err))
			}
			return
		}
		if n > 0 && s.log != nil {
			s.log.Info("audit: purged entries older than 6 months", slog.Int64("count", n))
		}
	}
	run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
