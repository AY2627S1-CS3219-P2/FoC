// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-11-09
// Scope: Regression tests for password-change access and refresh invalidation.
// Author review: ZI YANG — reviewed and verified

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"foc/user-service/internal/hash"

	"github.com/google/uuid"
)

func TestPasswordChangesPublishAccessCutoff(t *testing.T) {
	oldHash, err := hash.HashPassword("OldPass1")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"self", "admin", "audited admin"} {
		t.Run(mode, func(t *testing.T) {
			uid := uuid.New()
			at := time.Date(2026, 11, 9, 4, 0, 1, 750000000, time.UTC)
			repo := &fakeAccountRepository{user: &User{UID: uid, PasswordHash: oldHash, AccountStatus: AccountStatusActive}}
			writer := &fakeSuspensionWriter{}
			sessions := &fakeAccountSessionRepository{}
			ttl := 15*time.Minute + 5*time.Second
			svc := NewAccountServiceWithClock(repo, writer, sessions, ttl, func() time.Time { return at })
			var account *User
			var err error
			switch mode {
			case "self":
				account, err = svc.UpdateProfile(context.Background(), uid, "", "", "OldPass1", "NewPass1")
			case "admin":
				account, err = svc.UpdateProfileAsAdmin(context.Background(), uid, "", "", "NewPass1")
			case "audited admin":
				account, err = svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "", "", "NewPass1")
			}
			if err != nil {
				t.Fatal(err)
			}
			if writer.uid != uid || !writer.at.Equal(at.Truncate(time.Second)) || writer.ttl != ttl {
				t.Fatalf("access invalidation = %s, %s, %s; want target, whole-second cutoff, and access TTL plus skew", writer.uid, writer.at, writer.ttl)
			}
			if !account.TokensValidAfter.Equal(writer.at) || sessions.uid != uid {
				t.Fatal("password change must share the access cutoff and revoke refresh sessions")
			}
			if account.AccountStatus != AccountStatusActive {
				t.Fatal("password change changed account status")
			}
		})
	}
}

func TestPasswordChangeInvalidationFailuresDoNotPersistPassword(t *testing.T) {
	oldHash, err := hash.HashPassword("OldPass1")
	if err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("invalidation unavailable")
	for _, failure := range []string{"Redis", "refresh sessions", "missing writer", "missing sessions", "invalid TTL"} {
		t.Run(failure, func(t *testing.T) {
			uid := uuid.New()
			repo := &fakeAccountRepository{user: &User{UID: uid, PasswordHash: oldHash}}
			writer := &fakeSuspensionWriter{}
			sessions := &fakeAccountSessionRepository{}
			svc := NewAccountService(repo, writer, sessions, time.Minute)
			switch failure {
			case "Redis":
				writer.err = unavailable
			case "refresh sessions":
				sessions.err = unavailable
			case "missing writer":
				svc.suspensionWriter = nil
			case "missing sessions":
				svc.sessions = nil
			case "invalid TTL":
				svc.suspensionTTL = 0
			}
			_, err := svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "", "", "NewPass1")
			if err == nil {
				t.Fatal("password change succeeded without session invalidation")
			}
			if (failure == "Redis" || failure == "refresh sessions") && !errors.Is(err, unavailable) {
				t.Fatalf("error = %v, want wrapped invalidation failure", err)
			}
			if repo.updated != nil || len(repo.adminEntries) != 0 {
				t.Fatal("password or successful audit entry persisted despite invalidation failure")
			}
			if failure == "Redis" && sessions.uid != uuid.Nil {
				t.Fatal("refresh revocation ran after Redis failed")
			}
		})
	}
}

func TestProfileEditsAndRejectedPasswordsDoNotInvalidateSessions(t *testing.T) {
	oldHash, err := hash.HashPassword("OldPass1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, current, next string
		wantErr             error
	}{
		{name: "profile only"},
		{name: "incorrect current password", current: "WrongPass1", next: "NewPass1", wantErr: ErrInvalidCurrentPassword},
		{name: "invalid new password", current: "OldPass1", next: "weak", wantErr: ErrInvalidPassword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid := uuid.New()
			repo := &fakeAccountRepository{user: &User{UID: uid, PasswordHash: oldHash}}
			writer := &fakeSuspensionWriter{}
			sessions := &fakeAccountSessionRepository{}
			svc := NewAccountService(repo, writer, sessions, time.Minute)
			_, err := svc.UpdateProfile(context.Background(), uid, "alice", "", tc.current, tc.next)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if writer.uid != uuid.Nil || sessions.uid != uuid.Nil {
				t.Fatal("profile edit or rejected password invalidated sessions")
			}
		})
	}
}
