// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin side of AccountService (D-106 to D-123): audited suspend,
//   reactivate and profile edits, the rule that no admin is suspended (D-118,
//   D-119), the account search and list, and the audit log's read and purge.
// Author review: PENDING — <reviewer to complete>

package user

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PurgeInterval is how often the scheduled purge runs (D-115).
const PurgeInterval = 24 * time.Hour

// maxSearchLen bounds a search term; a username is at most 128 characters and
// an email 255, so a longer term can match nothing useful.
const maxSearchLen = 255

// AdminUpdateAccountStatus is UpdateAccountStatus for an admin: the status
// change and its audit row are saved together (D-108), suspending an admin or
// oneself is refused (D-118, D-119), and every refused or failed attempt is
// logged too (D-109).
func (s *AccountService) AdminUpdateAccountStatus(ctx context.Context, ac AuditContext, uid uuid.UUID, status AccountStatus) error {
	err := s.setAccountStatus(ctx, uid, status, &ac)
	if err != nil {
		action := ActionReactivate
		if status == AccountStatusSuspended {
			action = ActionSuspend
		}
		s.recordAttempt(ctx, ac, action, uid.String(), nil, err)
	}
	return err
}

// AdminUpdateProfile changes another account's username, phone number or
// password as an admin, with the audit rows saved in the same transaction
// (D-108). A password reset is logged as a change with no value (D-111).
func (s *AccountService) AdminUpdateProfile(ctx context.Context, ac AuditContext, uid uuid.UUID, username, phone, newPassword string) (*User, error) {
	account, err := s.updateProfile(ctx, uid, username, phone, newPassword, &ac, func(*User) error { return nil })
	if err != nil {
		action := ActionUpdate
		if newPassword != "" {
			action = ActionPasswordReset
		}
		s.recordAttempt(ctx, ac, action, uid.String(), attemptedProfile(username, phone), err)
		return nil, err
	}
	return account, nil
}

// attemptedProfile is what an attempted edit asked for, never the password.
func attemptedProfile(username, phone string) map[string]any {
	m := map[string]any{}
	if username != "" {
		m["username"] = username
	}
	if phone != "" {
		m["phone_num"] = phone
	}
	return m
}

// profileAuditEntries is the audit rows of one admin profile edit: a password
// reset row when the password changed, and an update row holding the new value
// of each field that changed (an edit that changed nothing still logs one).
func profileAuditEntries(ac AuditContext, uid uuid.UUID, changed map[string]any, passwordChanged bool) []*AuditEntry {
	var entries []*AuditEntry
	if passwordChanged {
		entries = append(entries, ac.entry(ActionPasswordReset, uid.String(), OutcomeSucceeded, ""))
	}
	if len(changed) > 0 || !passwordChanged {
		e := ac.entry(ActionUpdate, uid.String(), OutcomeSucceeded, "")
		e.AdditionalComments = changed
		entries = append(entries, e)
	}
	return entries
}

// recordAttempt writes the audit row for an attempt that did not commit
// (D-109). It cannot share the action's transaction, which rolled back or never
// began, so it is a write of its own; if that fails too the attempt is logged to
// stderr and the caller still gets the real error. The write ignores the
// request's cancellation, so a client that hangs up does not lose the row.
func (s *AccountService) recordAttempt(ctx context.Context, ac AuditContext, action, targetID string, comments map[string]any, cause error) {
	if s.repository == nil {
		return
	}
	outcome, reason := classify(cause)
	e := ac.entry(action, targetID, outcome, reason)
	e.AdditionalComments = comments

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repository.RecordAudit(ctx, e); err != nil {
		log.Printf("audit: could not record %s %s of %q by %s (%s): %v", action, outcome, targetID, ac.ActorID, reason, err)
	}
}

// SearchUsers returns one page of accounts for the admin (D-121 to D-123).
func (s *AccountService) SearchUsers(ctx context.Context, f UserFilter) (*UserPage, error) {
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	if f.Limit <= 0 || f.Limit > MaxPage {
		f.Limit = MaxPage
	}
	if f.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidFilter)
	}
	switch f.Status {
	case "", AccountStatusActive, AccountStatusSuspended, AccountStatusUnverified:
	default:
		return nil, fmt.Errorf("%w: status must be ACTIVE, SUSPENDED or UNVERIFIED", ErrInvalidFilter)
	}
	f.Query = strings.TrimSpace(f.Query)
	if len(f.Query) > maxSearchLen {
		return nil, fmt.Errorf("%w: search text is too long", ErrInvalidFilter)
	}
	page, err := s.repository.SearchUsers(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("search accounts: %w", err)
	}
	return page, nil
}

// AuditLog returns one page of the admin audit log, newest first (D-110).
func (s *AccountService) AuditLog(ctx context.Context, f AuditFilter) (*AuditPage, error) {
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	if f.Limit <= 0 || f.Limit > MaxPage {
		f.Limit = MaxPage
	}
	if f.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidFilter)
	}
	switch f.Outcome {
	case "", OutcomeSucceeded, OutcomeRefused, OutcomeFailed:
	default:
		return nil, fmt.Errorf("%w: outcome must be succeeded, refused or failed", ErrInvalidFilter)
	}
	page, err := s.repository.ListAudit(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	return page, nil
}

// RunAuditPurge removes the entries older than 6 months once now and then every
// interval, until ctx ends (D-115). The database refuses to delete anything
// newer (D-114). A failed run is logged and the next one tries again.
func (s *AccountService) RunAuditPurge(ctx context.Context, interval time.Duration) {
	run := func() {
		n, err := s.repository.PurgeAudit(ctx)
		if err != nil {
			log.Printf("audit: purge failed: %v", err)
			return
		}
		if n > 0 {
			log.Printf("audit: purged %d entries older than 6 months", n)
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
