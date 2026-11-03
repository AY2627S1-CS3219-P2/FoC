// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin audit record and the account search and list types for the
//   admin dashboard (D-106 to D-123): what an account action logs, and how a
//   refused or failed attempt is told apart from one that succeeded.
// Author review: PENDING — <reviewer to complete>

package user

import (
	"errors"
	"time"
)

// The audit actions this service records (D-106). The codes are the same shape
// in every service: <thing>.<verb>.
const (
	ActionSuspend       = "account.suspend"
	ActionReactivate    = "account.reactivate"
	ActionPasswordReset = "account.password_reset"
	ActionUpdate        = "account.update"

	// AuditTargetType is the audit target_type of every account action.
	AuditTargetType = "account"
)

// Audit outcomes (D-109).
const (
	OutcomeSucceeded = "succeeded"
	OutcomeRefused   = "refused"
	OutcomeFailed    = "failed"
)

// MaxPage is the most rows one page of a list returns (D-104).
const MaxPage = 50

// maxReasonLen keeps an error text from filling a log row.
const maxReasonLen = 300

var (
	// ErrCannotSuspendAdmin refuses suspending an admin account (D-118).
	ErrCannotSuspendAdmin = errors.New("an admin account cannot be suspended")
	// ErrCannotSuspendSelf refuses an admin suspending their own account (D-119).
	ErrCannotSuspendSelf = errors.New("you cannot suspend your own account")
	// ErrInvalidFilter refuses a list or audit query value that makes no sense.
	ErrInvalidFilter = errors.New("invalid filter")
)

// AuditContext says who an audited action is attributed to. The HTTP layer
// builds it from the verified token (D-041) and the request id.
type AuditContext struct {
	ActorID       string
	ActorRole     string
	CorrelationID string
}

// AuditEntry is one row of the admin audit log (D-107, D-113); the shape is the
// same in every service, so the dashboard merges them by OccurredAt.
type AuditEntry struct {
	ID         string
	OccurredAt time.Time
	ActorID    string
	ActorRole  string
	Action     string
	TargetType string
	TargetID   string
	Outcome    string
	// RefusalReason says why an attempt was refused or failed; empty when it
	// succeeded.
	RefusalReason string
	// Reason is the admin's own reason (D-116), empty unless one is taken.
	Reason string
	// BeforeState and AfterState hold a status change, such as
	// {"account_status": "ACTIVE"} (D-113).
	BeforeState map[string]any
	AfterState  map[string]any
	// AdditionalComments holds the new value of each field a profile edit set;
	// a password reset holds nothing (D-111).
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

// UserFilter narrows the admin's account search and list (D-121 to D-123).
type UserFilter struct {
	// Query matches anywhere in the username or email, ignoring case.
	Query string
	// Status limits the list to one account status; empty means every status.
	Status AccountStatus
	Limit  int
	Offset int
}

// UserPage is one page of accounts with the total that matched.
type UserPage struct {
	Items  []*User
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
		TargetType:    AuditTargetType,
		TargetID:      targetID,
		Outcome:       outcome,
		RefusalReason: reason,
		CorrelationID: ac.CorrelationID,
	}
}

// EntryForTest builds a succeeded audit row for ac; tests in other packages use
// it to write rows, since the service builds its own.
func (ac AuditContext) EntryForTest(action, targetID string) *AuditEntry {
	return ac.entry(action, targetID, OutcomeSucceeded, "")
}

// classify tells a refusal, which a rule or the caller's input caused, from a
// failure, which it did not (D-109). A failure's text is cut short.
func classify(err error) (outcome, reason string) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrCannotSuspendAdmin),
		errors.Is(err, ErrCannotSuspendSelf), errors.Is(err, ErrLastAdmin),
		errors.Is(err, ErrAccountUnverified), errors.Is(err, ErrInvalidUsername),
		errors.Is(err, ErrInvalidPhone), errors.Is(err, ErrInvalidPassword),
		errors.Is(err, ErrDuplicateUsername):
		return OutcomeRefused, err.Error()
	}
	reason = err.Error()
	if len(reason) > maxReasonLen {
		reason = reason[:maxReasonLen]
	}
	return OutcomeFailed, reason
}
