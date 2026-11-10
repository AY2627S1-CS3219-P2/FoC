// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Approved administrator role policy and audited role-change service.
// Author review: ZI YANG - reviewed and verified

package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Role-change refusal errors are mapped to the approved HTTP contract.
var (
	ErrInvalidRole       = errors.New("invalid account role")
	ErrAdminRequired     = errors.New("active admin access required")
	ErrCannotDemoteSelf  = errors.New("you cannot demote your own account")
	ErrLastActiveAdmin   = errors.New("cannot demote the last active admin")
	ErrInactivePromotion = errors.New("only an active student can be promoted")
)

// ActionChangeRole identifies role changes in the existing account audit log.
const ActionChangeRole = "account.change_role"

// RoleChangeRepository commits role, cutoff, session revocation and audit together.
// It invokes invalidate only for a permitted, actual change, under its locks.
type RoleChangeRepository interface {
	ChangeRoleAudited(ctx context.Context, actorID, targetID uuid.UUID, role AccountRole, entry *AuditEntry, invalidate func() (time.Time, error)) error
}

// ValidateRoleChange evaluates the policy using state read under transaction locks.
func ValidateRoleChange(actor, target *User, role AccountRole, activeAdmins int) error {
	if actor == nil || actor.AccountRole != AccountRoleAdmin || actor.AccountStatus != AccountStatusActive {
		return ErrAdminRequired
	}
	if role != AccountRoleAdmin && role != AccountRoleStudent {
		return ErrInvalidRole
	}
	if target == nil {
		return ErrNotFound
	}
	if actor.UID == target.UID && role == AccountRoleStudent {
		return ErrCannotDemoteSelf
	}
	if target.AccountRole == role {
		return nil
	}
	if role == AccountRoleAdmin && target.AccountStatus != AccountStatusActive {
		return ErrInactivePromotion
	}
	if role == AccountRoleStudent && target.AccountRole == AccountRoleAdmin && target.AccountStatus == AccountStatusActive && activeAdmins <= 1 {
		return ErrLastActiveAdmin
	}
	return nil
}

// AdminUpdateRole changes another account's role and invalidates its credentials.
func (s *AccountService) AdminUpdateRole(ctx context.Context, ac AuditContext, uid uuid.UUID, role AccountRole) (err error) {
	defer func() {
		if err != nil {
			s.recordAttempt(ctx, ac, ActionChangeRole, uid.String(), map[string]any{AuditFieldsRequested: []string{"account_role"}}, err)
		}
	}()
	actorID, parseErr := uuid.Parse(ac.ActorID)
	if parseErr != nil || ac.ActorRole != string(AccountRoleAdmin) {
		return ErrAdminRequired
	}
	if role != AccountRoleAdmin && role != AccountRoleStudent {
		return ErrInvalidRole
	}
	if actorID == uid && role == AccountRoleStudent {
		return ErrCannotDemoteSelf
	}
	repo, ok := s.repository.(RoleChangeRepository)
	if !ok || s.suspensionWriter == nil || s.suspensionTTL <= 0 {
		return errors.New("role change dependencies are required")
	}
	entry := ac.entry(ActionChangeRole, uid.String(), OutcomeSucceeded, "")
	err = repo.ChangeRoleAudited(ctx, actorID, uid, role, entry, func() (time.Time, error) {
		cutoff := s.tokenCutoff()
		if writeErr := s.suspensionWriter.WriteSuspension(ctx, uid, cutoff, s.suspensionTTL); writeErr != nil {
			return time.Time{}, fmt.Errorf("invalidate role credentials: %w", writeErr)
		}
		return cutoff, nil
	})
	if err != nil {
		return fmt.Errorf("change account role: %w", err)
	}
	return nil
}
