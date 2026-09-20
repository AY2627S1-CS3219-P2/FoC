// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for registration, profile updates, and account-status transitions.
// Author review: PENDING — reviewer to complete

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccountServiceRegisterHashesPasswordAndSetsDefaults(t *testing.T) {
	repository := &fakeAuthRepository{}
	service := NewAccountService(repository)

	account, err := service.Register(context.Background(), "student@example.com", "student", "ValidPass1")
	if err != nil {
		t.Fatal(err)
	}
	if repository.user != account {
		t.Fatalf("created account pointer = %p, want %p", repository.user, account)
	}
	if account.Email != "student@example.com" || account.Username != "student" || account.AccountRole != AccountRoleStudent || account.AccountStatus != AccountStatusActive {
		t.Fatalf("created account = %#v", account)
	}
	if account.PasswordHash == "ValidPass1" || !CheckPassword(account.PasswordHash, "ValidPass1") {
		t.Fatal("registration password was not stored as a valid hash")
	}
}

func TestAccountServiceUpdateProfilePreservesEmptyPassword(t *testing.T) {
	uid := uuid.New()
	oldHash, err := HashPassword("OldPass1")
	if err != nil {
		t.Fatal(err)
	}
	account := &User{UID: uid, Username: "old", PasswordHash: oldHash}
	repository := &fakeAuthRepository{user: account}
	service := NewAccountService(repository)

	updated, err := service.UpdateProfile(context.Background(), uid, "new", "+6512345678", "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "new" || updated.PhoneNum != "+6512345678" || updated.PasswordHash != oldHash {
		t.Fatalf("updated account = %#v", updated)
	}
	if repository.updated != account {
		t.Fatal("repository did not receive the updated account")
	}
}

func TestAccountServiceUpdateStatusDelegatesRecordedTransitions(t *testing.T) {
	repository := &fakeAuthRepository{}
	service := NewAccountService(repository)
	timestamp := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)

	if err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatusSuspended, timestamp); err != nil {
		t.Fatal(err)
	}
	if repository.status != AccountStatusSuspended {
		t.Fatalf("status = %q, want suspended", repository.status)
	}
	if err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatus("UNKNOWN"), timestamp); err == nil {
		t.Fatal("invalid status was accepted")
	}
	if err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatusSuspended, time.Time{}); err == nil {
		t.Fatal("suspension without timestamp was accepted")
	}
}

func TestAccountServicePropagatesRepositoryErrors(t *testing.T) {
	failure := errors.New("database unavailable")
	repository := &fakeAuthRepository{lookupErr: failure}
	service := NewAccountService(repository)

	_, err := service.UpdateProfile(context.Background(), uuid.New(), "new", "", "")
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want repository error", err)
	}
}
