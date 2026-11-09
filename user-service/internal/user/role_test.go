// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Approved role policies, invalidation failures and audit coverage.
// Author review: ZI YANG - reviewed and verified

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateRoleChange(t *testing.T) {
	actor := &User{UID: uuid.New(), AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive}
	for _, tc := range []struct {
		name          string
		actor, target *User
		role          AccountRole
		count         int
		want          error
	}{
		{"promote active student", actor, &User{UID: uuid.New(), AccountRole: AccountRoleStudent, AccountStatus: AccountStatusActive}, AccountRoleAdmin, 1, nil},
		{"demote another admin", actor, &User{UID: uuid.New(), AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive}, AccountRoleStudent, 2, nil},
		{"self demotion with another admin", actor, actor, AccountRoleStudent, 2, ErrCannotDemoteSelf},
		{"self demotion as last admin", actor, actor, AccountRoleStudent, 1, ErrCannotDemoteSelf},
		{"suspended student cannot promote", actor, &User{UID: uuid.New(), AccountRole: AccountRoleStudent, AccountStatus: AccountStatusSuspended}, AccountRoleAdmin, 1, ErrInactivePromotion},
		{"unverified student cannot promote", actor, &User{UID: uuid.New(), AccountRole: AccountRoleStudent, AccountStatus: AccountStatusUnverified}, AccountRoleAdmin, 1, ErrInactivePromotion},
		{"last active admin", actor, &User{UID: uuid.New(), AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive}, AccountRoleStudent, 1, ErrLastActiveAdmin},
		{"student actor", &User{AccountRole: AccountRoleStudent, AccountStatus: AccountStatusActive}, actor, AccountRoleStudent, 2, ErrAdminRequired},
		{"suspended actor", &User{AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusSuspended}, actor, AccountRoleStudent, 2, ErrAdminRequired},
		{"missing actor", nil, actor, AccountRoleStudent, 2, ErrAdminRequired},
		{"missing target", actor, nil, AccountRoleAdmin, 1, ErrNotFound},
		{"invalid role", actor, actor, "COURIER", 1, ErrInvalidRole},
		{"unchanged role", actor, actor, AccountRoleAdmin, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRoleChange(tc.actor, tc.target, tc.role, tc.count); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

type fakeRoleRepository struct {
	fakeAccountRepository
	actor, target *User
	count         int
	err           error
	called        bool
	entry         *AuditEntry
}

func (r *fakeRoleRepository) ChangeRoleAudited(_ context.Context, _, _ uuid.UUID, role AccountRole, e *AuditEntry, invalidate func() (time.Time, error)) error {
	r.called = true
	if r.err != nil {
		return r.err
	}
	if err := ValidateRoleChange(r.actor, r.target, role, r.count); err != nil {
		return err
	}
	if r.target.AccountRole != role {
		cutoff, err := invalidate()
		if err != nil {
			return err
		}
		r.target.AccountRole = role
		r.target.TokensValidAfter = cutoff
	}
	r.entry = e
	return nil
}
func TestAdminUpdateRoleInvalidationAndAttempts(t *testing.T) {
	for _, tc := range []struct {
		name               string
		repoErr, writerErr error
		wantRole           AccountRole
	}{
		{"success", nil, nil, AccountRoleAdmin},
		{"Redis failure", nil, errors.New("Redis unavailable"), AccountRoleStudent},
		{"database failure", errors.New("database unavailable"), nil, AccountRoleStudent},
		{"policy refusal", ErrInactivePromotion, nil, AccountRoleStudent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := &User{UID: uuid.New(), AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive}
			target := &User{UID: uuid.New(), AccountRole: AccountRoleStudent, AccountStatus: AccountStatusActive}
			repo := &fakeRoleRepository{actor: actor, target: target, count: 1, err: tc.repoErr}
			writer := &fakeSuspensionWriter{err: tc.writerErr}
			at := time.Now().UTC().Truncate(time.Second)
			svc := NewAccountServiceWithClock(repo, writer, nil, time.Minute, func() time.Time { return at })
			err := svc.AdminUpdateRole(context.Background(), AuditContext{ActorID: actor.UID.String(), ActorRole: "ADMIN"}, target.UID, AccountRoleAdmin)
			if target.AccountRole != tc.wantRole {
				t.Fatalf("role = %s", target.AccountRole)
			}
			if tc.repoErr == nil && tc.writerErr == nil {
				if err != nil || repo.entry == nil || repo.entry.Action != ActionChangeRole || writer.uid != target.UID || !target.TokensValidAfter.Equal(at) {
					t.Fatalf("change = %v, entry=%+v", err, repo.entry)
				}
			} else {
				if err == nil || len(repo.audits) != 1 {
					t.Fatalf("failed attempt = %v, audits=%d", err, len(repo.audits))
				}
				if tc.repoErr == ErrInactivePromotion && repo.audits[0].Outcome != OutcomeRefused {
					t.Fatal("policy refusal not audited as refused")
				}
			}
		})
	}
}
func TestAdminCannotDemoteSelfBeforeSideEffects(t *testing.T) {
	repo := &fakeRoleRepository{}
	writer := &fakeSuspensionWriter{}
	actorID := uuid.New()
	svc := NewAccountService(repo, writer, nil, time.Minute)
	err := svc.AdminUpdateRole(context.Background(), AuditContext{ActorID: actorID.String(), ActorRole: "ADMIN"}, actorID, AccountRoleStudent)
	if !errors.Is(err, ErrCannotDemoteSelf) || repo.called || writer.uid != uuid.Nil || len(repo.audits) != 1 {
		t.Fatalf("self demotion = %v, repository called=%t", err, repo.called)
	}
}
