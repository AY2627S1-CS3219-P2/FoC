// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin audit record and the admin list and count types for the admin
//   dashboard (D-104 to D-117): what an admin's cancel or complete logs, how a
//   refused or failed attempt is told apart, and the past-errands filter.
// Author review: PENDING — <reviewer to complete>

package order

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// The audit actions this service records (D-106). The codes are the same shape
// in every service: <thing>.<verb>.
const (
	AuditCancel   = "errand.cancel"
	AuditComplete = "errand.complete"

	// AuditTargetType is the audit target_type of every errand action.
	AuditTargetType = "errand"
)

// Audit outcomes (D-109).
const (
	OutcomeSucceeded = "succeeded"
	OutcomeRefused   = "refused"
	OutcomeFailed    = "failed"
)

// MaxAdminPage is the most rows one page of an admin list returns (D-104).
const MaxAdminPage = 50

// maxReasonLen keeps an error text from filling a log row.
const maxReasonLen = 300

// Sort orders of the admin errand list (D-105).
const (
	SortRecent       = "recent"
	SortSupplierName = "supplier_name"
	SortOldest       = "oldest"
)

// AuditContext says who an audited action is attributed to: the verified caller
// (D-041) and the request's correlation id.
type AuditContext struct {
	ActorID       string
	ActorRole     string
	CorrelationID string
}

// AuditEntry is one row of the admin audit log (D-107, D-113); the shape is the
// same in every service, so the dashboard merges them by OccurredAt.
type AuditEntry struct {
	ID            string
	OccurredAt    time.Time
	ActorID       string
	ActorRole     string
	Action        string
	TargetType    string
	TargetID      string
	Outcome       string
	RefusalReason string
	// Reason is the admin's own reason (D-116), empty unless one is taken.
	Reason string
	// BeforeState and AfterState hold the errand's state, such as {"state": "OPEN"}.
	BeforeState        map[string]any
	AfterState         map[string]any
	AdditionalComments map[string]any
	CorrelationID      string
}

// AuditFilter narrows the audit list and pages it (D-104).
type AuditFilter struct {
	Action   string
	Outcome  string
	ActorID  string
	TargetID string
	From, To *time.Time
	Limit    int
	Offset   int
}

// AuditPage is one page of the audit log with the total that matched.
type AuditPage struct {
	Items  []*AuditEntry
	Total  int
	Limit  int
	Offset int
}

// AdminErrandFilter narrows the admin's errand list (D-105, D-117).
type AdminErrandFilter struct {
	Scope Scope
	// State limits the list to one state; nil means every state of the scope.
	State *State
	// From and To bound when the errand reached its state: From inclusive, To
	// exclusive.
	From, To   *time.Time
	SupplierID *uuid.UUID
	// UserID matches errands this user requested or is assigned to.
	UserID *uuid.UUID
	Sort   string
	Limit  int
	Offset int
}

// AdminErrandPage is one page of the admin's errand list with the total that
// matched.
type AdminErrandPage struct {
	Items  []Order
	Total  int
	Limit  int
	Offset int
}

// ErrandCounts is how many errands a user has in each state, as requester and
// as the courier now assigned (D-117). A state with none is absent.
type ErrandCounts struct {
	Requested map[State]int
	Assigned  map[State]int
}

// adminAuditEntry is the audit row of an admin's cancel or complete, saved in
// the same transaction as the transition (D-108). It returns nil for any other
// action, which an admin cannot take.
func adminAuditEntry(c *Caller, a Action, o Order, to State, now time.Time, correlationID string) *AuditEntry {
	code := auditCode(a)
	if c == nil || code == "" {
		return nil
	}
	return &AuditEntry{
		OccurredAt:    now,
		ActorID:       c.UserID.String(),
		ActorRole:     string(c.Role),
		Action:        code,
		TargetType:    AuditTargetType,
		TargetID:      o.ID.String(),
		Outcome:       OutcomeSucceeded,
		BeforeState:   map[string]any{"state": string(o.State)},
		AfterState:    map[string]any{"state": string(to)},
		CorrelationID: correlationID,
	}
}

func auditCode(a Action) string {
	switch a {
	case ActionAdminCancel:
		return AuditCancel
	case ActionAdminComplete:
		return AuditComplete
	}
	return ""
}

// classify tells a refusal, which a rule or the caller's input caused, from a
// failure, which it did not (D-109). A failure's text is cut short.
func classify(err error) (outcome, reason string) {
	switch KindOf(err) {
	case KindValidation, KindForbidden, KindNotFound, KindConflict:
		return OutcomeRefused, MessageOf(err)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNoMatch) {
		return OutcomeRefused, err.Error()
	}
	reason = err.Error()
	if len(reason) > maxReasonLen {
		reason = reason[:maxReasonLen]
	}
	return OutcomeFailed, reason
}
