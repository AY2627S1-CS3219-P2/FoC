// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin audit record and the rules for building one (D-106 to D-115):
//   what a supplier create, edit and delete logs, and how a refused or failed
//   attempt is told apart from one that succeeded.
// Author review: PENDING — <reviewer to complete>

package supplier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// The audit actions this service records (D-106). The codes are the same shape
// in every service: <thing>.<verb>.
const (
	ActionCreate = "supplier.create"
	ActionUpdate = "supplier.update"
	ActionDelete = "supplier.delete"

	// TargetType is the audit target_type of every supplier action.
	TargetType = "supplier"
)

// Audit outcomes (D-109): an attempt either happened, was refused by a rule,
// or failed for a reason that is not the caller's doing.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeRefused   = "refused"
	OutcomeFailed    = "failed"
)

// maxReasonLen keeps an error text from filling a log row.
const maxReasonLen = 300

// AuditContext says who an audited action is attributed to. The HTTP layer
// builds it from the verified token (D-041) and the request id.
type AuditContext struct {
	ActorID       string
	ActorRole     string
	CorrelationID string
}

// AuditEntry is one row of the admin audit log (D-107, D-113). The record shape
// is the same in user-service and order-service, so the dashboard can merge
// them by occurred_at.
type AuditEntry struct {
	ID         string
	OccurredAt time.Time
	ActorID    string
	ActorRole  string
	Action     string
	TargetType string
	// TargetID is empty when the attempt never named a row, such as a create
	// that was refused before it got an id.
	TargetID string
	Outcome  string
	// RefusalReason says why an attempt was refused or failed; empty when it
	// succeeded.
	RefusalReason string
	// Reason is the admin's own reason (D-116), empty unless one is taken.
	Reason string
	// BeforeState and AfterState hold a status change (D-113). A supplier action
	// is not one, so they stay nil here.
	BeforeState map[string]any
	AfterState  map[string]any
	// AdditionalComments holds the new value of every field an edit changed
	// (D-111, D-112); for a create, the fields it set.
	AdditionalComments map[string]any
	CorrelationID      string
}

// AuditFilter narrows the audit list and pages it (D-104, D-105 style: offset
// paging, at most 50 rows a page).
type AuditFilter struct {
	Action   string
	Outcome  string
	ActorID  string
	TargetID string
	From, To *time.Time
	Limit    int
	Offset   int
}

// MaxAuditPage is the most rows one page of the audit log returns (D-104).
const MaxAuditPage = 50

// AuditPage is one page of the audit log with the total that matched.
type AuditPage struct {
	Items  []*AuditEntry
	Total  int
	Limit  int
	Offset int
}

func (ac AuditContext) entry(action, targetID, outcome, reason string) *AuditEntry {
	return &AuditEntry{
		OccurredAt:    time.Now().UTC(),
		ActorID:       ac.ActorID,
		ActorRole:     ac.ActorRole,
		Action:        action,
		TargetType:    TargetType,
		TargetID:      targetID,
		Outcome:       outcome,
		RefusalReason: reason,
		CorrelationID: ac.CorrelationID,
	}
}

// supplierFields is a supplier's editable fields under their API names, so a
// log row reads like the request an admin sent.
func supplierFields(s *Supplier) map[string]any {
	return map[string]any{
		"name":                 s.Name,
		"type":                 s.Type,
		"building":             s.Building,
		"floor":                s.Floor,
		"location_description": s.LocationDescription,
		"latitude":             s.Latitude,
		"longitude":            s.Longitude,
		"opening_time":         s.OpeningTime,
		"closing_time":         s.ClosingTime,
		"image_url":            s.ImageURL,
		"description":          s.Description,
		"is_available":         s.IsAvailable,
	}
}

// changedFields returns the new value of every field that differs between
// before and after (D-112).
func changedFields(before, after *Supplier) map[string]any {
	was, now := supplierFields(before), supplierFields(after)
	changed := map[string]any{}
	for k, v := range now {
		if was[k] != v {
			changed[k] = v
		}
	}
	return changed
}

// classify tells a refusal, which a rule or the caller's input caused, from a
// failure, which it did not (D-109). A failure's text is cut short so a long
// driver error cannot fill the row.
func classify(err error) (outcome, reason string) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrValidation),
		errors.Is(err, ErrDuplicate), errors.Is(err, ErrInUse):
		return OutcomeRefused, err.Error()
	}
	reason = err.Error()
	if len(reason) > maxReasonLen {
		reason = reason[:maxReasonLen]
	}
	return OutcomeFailed, reason
}

// recordAttempt writes the audit row for an attempt that did not commit
// (D-109). It cannot share the action's transaction, which rolled back, so it
// is a write of its own; if that write fails too the attempt is logged at
// ERROR and the original error still reaches the caller. The write ignores the
// request's cancellation, so a client that hangs up does not lose the row.
func (s *Service) recordAttempt(ctx context.Context, ac AuditContext, action, targetID string, comments map[string]any, cause error) {
	outcome, reason := classify(cause)
	e := ac.entry(action, targetID, outcome, reason)
	e.AdditionalComments = comments

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repo.RecordAudit(ctx, e); err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, "could not record an audit row",
			slog.String("request_id", ac.CorrelationID),
			slog.String("action", action),
			slog.String("outcome", outcome),
			slog.String("target_id", targetID),
			slog.String("user_id", ac.ActorID),
			slog.String("reason", reason),
			slog.String("err", err.Error()))
	}
}

// AuditLog returns one page of the admin audit log, newest first.
func (s *Service) AuditLog(ctx context.Context, f AuditFilter) (*AuditPage, error) {
	if f.Limit <= 0 || f.Limit > MaxAuditPage {
		f.Limit = MaxAuditPage
	}
	if f.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrValidation)
	}
	if f.Outcome != "" && f.Outcome != OutcomeSucceeded && f.Outcome != OutcomeRefused && f.Outcome != OutcomeFailed {
		return nil, fmt.Errorf("%w: outcome must be succeeded, refused or failed", ErrValidation)
	}
	return s.repo.ListAudit(ctx, f)
}

// PurgeAudit removes the entries older than 6 months (D-115). The database
// refuses to delete anything newer (D-114).
func (s *Service) PurgeAudit(ctx context.Context) (int64, error) {
	return s.repo.PurgeAudit(ctx)
}

// PurgeInterval is how often the scheduled purge runs (D-115).
const PurgeInterval = 24 * time.Hour

// RunAuditPurge removes the entries older than 6 months once now and then every
// interval, until ctx ends (D-115). A failed run is logged and the next one
// tries again; it never stops the service.
func (s *Service) RunAuditPurge(ctx context.Context, interval time.Duration) {
	run := func() {
		n, err := s.PurgeAudit(ctx)
		if err != nil {
			s.log.Error("audit purge failed", "err", err)
			return
		}
		if n > 0 {
			s.log.Info("audit purge removed entries older than 6 months", "removed", n)
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
