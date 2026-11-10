// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The admin side of credit-service (D-150 to D-153): the flagged-accounts
//   list, an account's wallet as an admin sees it, Re-verify, and the admin
//   audit log; the audit record has the same shape as the other services' (D-107, D-113).
// Author review: Reviewed by Seth

package credit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// The audit action this service records (D-106, D-152). The codes are the same
// shape in every service: <thing>.<verb>.
const (
	AuditReverify = "credit.reverify"

	// AuditTargetType is the audit target_type of a Re-verify: the account.
	AuditTargetType = "account"
)

// Audit outcomes (D-109).
const (
	OutcomeSucceeded = "succeeded"
	OutcomeRefused   = "refused"
	OutcomeFailed    = "failed"
)

// MaxAdminPage is the most rows one page of an admin list returns (D-104).
const MaxAdminPage = 50

// AuditPurgeInterval is how often the purge of entries older than 6 months runs (D-115).
const AuditPurgeInterval = 24 * time.Hour

// maxReasonLen keeps an error text from filling a log row.
const maxReasonLen = 300

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
	// BeforeState and AfterState hold the account's integrity status, such as
	// {"integrity_status": "FLAGGED"}.
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

// Finding is what the ledger checks found wrong with an account: the stable
// reason code, a bounded detail, and the amounts that disagree where there are
// two (an out-of-bounds balance has only an observed one).
type Finding struct {
	ReasonCode string
	Detail     string
	Expected   *float64
	Observed   *float64
}

// FlaggedAccount is one row of the admin's list of flagged accounts (D-150).
// The reason is the first finding, which a later one never replaces (D-153).
type FlaggedAccount struct {
	UserID     uuid.UUID
	ReasonCode string
	Detail     *string
	Expected   *float64
	Observed   *float64
	FlaggedAt  time.Time
}

// FlaggedPage is one page of the flagged accounts with the total that matched.
type FlaggedPage struct {
	Items  []FlaggedAccount
	Total  int
	Limit  int
	Offset int
}

// ReverifyOutcome says what a Re-verify did (D-150).
type ReverifyOutcome string

// The four ways a Re-verify ends. Only Cleared changes the account.
const (
	// ReverifyCleared: the account was flagged, every check now passes, and it is CONSISTENT.
	ReverifyCleared ReverifyOutcome = "CLEARED"
	// ReverifyStillFlagged: the account was flagged and a check still fails. Finding says which.
	ReverifyStillFlagged ReverifyOutcome = "STILL_FLAGGED"
	// ReverifyNotFlagged: the account was not flagged, so there was nothing to clear.
	ReverifyNotFlagged ReverifyOutcome = "NOT_FLAGGED"
	// ReverifyStale: the account changed while it was being checked; nothing was written.
	ReverifyStale ReverifyOutcome = "STALE"
)

// ReverifyResult is the outcome of one Re-verify, with the current finding when
// the account is still flagged (D-153: it is returned, never stored).
type ReverifyResult struct {
	Outcome ReverifyOutcome
	Finding *Finding
}

// AdminRepository is what the admin routes read and write (D-150 to D-153).
type AdminRepository interface {
	// ListFlaggedAccounts returns the flagged accounts, oldest flag first.
	ListFlaggedAccounts(ctx context.Context, limit, offset int) (*FlaggedPage, error)
	// GetAdminAccount returns the account row as it is stored, flagged or not.
	// It returns ErrWalletNotReady when the user has none.
	GetAdminAccount(ctx context.Context, userID uuid.UUID) (*Account, error)
	// ReverifyAccount re-runs the ledger checks on one account. When it was
	// flagged and every check passes it clears the flag and writes entry as a
	// succeeded audit row in the same transaction; in every other outcome it
	// writes nothing. It returns ErrWalletNotReady when the user has no account.
	ReverifyAccount(ctx context.Context, userID uuid.UUID, entry *AuditEntry) (*ReverifyResult, error)
	// RecordAudit writes one audit row on its own, for an attempt that did not commit.
	RecordAudit(ctx context.Context, e *AuditEntry) error
	// ListAudit returns one page of the audit log, newest first.
	ListAudit(ctx context.Context, f AuditFilter) (*AuditPage, error)
	// PurgeAudit deletes the audit rows older than 6 months and returns how many.
	PurgeAudit(ctx context.Context) (int64, error)
}

// WithAdminRepository injects a specific AdminRepository.
func WithAdminRepository(repo AdminRepository) func(*Service) {
	return func(s *Service) {
		s.adminRepo = repo
	}
}

// WithLogger gives the service a logger for the audit writes that fail. Without
// one those failures are not logged.
func WithLogger(log *slog.Logger) func(*Service) {
	return func(s *Service) {
		s.log = log
	}
}

// ListFlaggedAccounts returns one page of the flagged accounts (D-150).
func (s *Service) ListFlaggedAccounts(ctx context.Context, limit, offset int) (*FlaggedPage, error) {
	if s.adminRepo == nil {
		return nil, ErrServiceUnavailable
	}
	if limit <= 0 || limit > MaxAdminPage {
		limit = MaxAdminPage
	}
	if offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidRequest)
	}
	return s.adminRepo.ListFlaggedAccounts(ctx, limit, offset)
}

// GetAdminAccount returns a user's account as stored, with its real balances
// even when it is flagged (D-150). The caller labels them untrusted then.
func (s *Service) GetAdminAccount(ctx context.Context, userID uuid.UUID) (*Account, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}
	if s.adminRepo == nil {
		return nil, ErrServiceUnavailable
	}
	return s.adminRepo.GetAdminAccount(ctx, userID)
}

// ReverifyAccount re-runs the ledger checks on one account and clears its flag
// when they all pass (D-150). A Re-verify that changes nothing because the
// checks still fail, or because the account changed meanwhile, is logged as
// refused (D-152); one of an account that is not flagged is not logged.
func (s *Service) ReverifyAccount(ctx context.Context, ac AuditContext, userID uuid.UUID) (*ReverifyResult, error) {
	if userID == uuid.Nil {
		return nil, ErrInvalidUserID
	}
	if s.adminRepo == nil {
		return nil, ErrServiceUnavailable
	}
	entry := &AuditEntry{
		OccurredAt:    time.Now().UTC(),
		ActorID:       ac.ActorID,
		ActorRole:     ac.ActorRole,
		Action:        AuditReverify,
		TargetType:    AuditTargetType,
		TargetID:      userID.String(),
		Outcome:       OutcomeSucceeded,
		CorrelationID: ac.CorrelationID,
	}
	result, err := s.adminRepo.ReverifyAccount(ctx, userID, entry)
	if err != nil {
		outcome, reason := classify(err)
		s.recordAttempt(ctx, entry, outcome, reason)
		return nil, err
	}
	switch result.Outcome {
	case ReverifyStillFlagged:
		reason := "a ledger check still fails"
		if f := result.Finding; f != nil {
			reason = truncate(f.ReasonCode + ": " + f.Detail)
		}
		s.recordAttempt(ctx, entry, OutcomeRefused, reason)
	case ReverifyStale:
		s.recordAttempt(ctx, entry, OutcomeRefused, "the account changed while it was being checked")
	}
	return result, nil
}

// ListAuditLog returns one page of the admin audit log, newest first (D-110).
func (s *Service) ListAuditLog(ctx context.Context, f AuditFilter) (*AuditPage, error) {
	if s.adminRepo == nil {
		return nil, ErrServiceUnavailable
	}
	if f.Limit <= 0 || f.Limit > MaxAdminPage {
		f.Limit = MaxAdminPage
	}
	if f.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidRequest)
	}
	switch f.Outcome {
	case "", OutcomeSucceeded, OutcomeRefused, OutcomeFailed:
	default:
		return nil, fmt.Errorf("%w: outcome must be succeeded, refused or failed", ErrInvalidRequest)
	}
	return s.adminRepo.ListAudit(ctx, f)
}

// recordAttempt writes the audit row for a Re-verify that did not commit a
// change (D-109). It cannot share a transaction with the clear, which did not
// happen, so it is a write of its own; if that fails too the attempt is logged
// and the caller still gets the real answer. The write ignores the request's
// cancellation, so a client that hangs up does not lose the row.
func (s *Service) recordAttempt(ctx context.Context, entry *AuditEntry, outcome, reason string) {
	e := *entry
	e.Outcome = outcome
	e.RefusalReason = reason
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.adminRepo.RecordAudit(ctx, &e); err != nil && s.log != nil {
		s.log.Error("audit: could not record a Re-verify attempt", slog.String("outcome", outcome),
			slog.String("account", e.TargetID), slog.String("actor", e.ActorID), slog.Any("error", err))
	}
}

// RunAuditPurge removes the entries older than 6 months once now and then every
// interval, until ctx ends (D-115). The database refuses to delete anything
// newer (D-114). A failed run is logged and the next one tries again.
func (s *Service) RunAuditPurge(ctx context.Context, interval time.Duration) {
	if s.adminRepo == nil {
		return
	}
	run := func() {
		n, err := s.adminRepo.PurgeAudit(ctx)
		if err != nil {
			if s.log != nil && ctx.Err() == nil {
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

// classify tells a refusal, which the caller's input or a rule caused, from a
// failure, which it did not (D-109). A failure's text is cut short.
func classify(err error) (outcome, reason string) {
	switch {
	case errors.Is(err, ErrWalletNotReady):
		return OutcomeRefused, "the user has no credit account"
	case errors.Is(err, ErrInvalidUserID), errors.Is(err, ErrInvalidRequest):
		return OutcomeRefused, err.Error()
	}
	return OutcomeFailed, truncate(err.Error())
}

func truncate(s string) string {
	if len(s) > maxReasonLen {
		return s[:maxReasonLen]
	}
	return s
}
