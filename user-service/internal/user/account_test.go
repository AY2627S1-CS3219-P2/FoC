// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for registration, profile updates, and account-status transitions.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"foc/user-service/internal/hash"
	"github.com/google/uuid"
)

func TestAccountServiceRegisterHashesPasswordAndSetsDefaults(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

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
	if account.PasswordHash == "ValidPass1" || !hash.CheckPassword(account.PasswordHash, "ValidPass1") {
		t.Fatal("registration password was not stored as a valid hash")
	}
}

// AI-generated (edited by ZI YANG).
func TestAccountServiceRegisterCanonicalizesEmail(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	account, err := service.Register(context.Background(), " Student@U.NUS.EDU ", "student", "ValidPass1")
	if err != nil {
		t.Fatal(err)
	}
	if account.Email != "student@u.nus.edu" {
		t.Fatalf("created account email = %q, want %q", account.Email, "student@u.nus.edu")
	}
}

func TestAccountServiceRegisterRejectsInvalidUsername(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.Register(context.Background(), "student@example.com", "student_user", "ValidPass1")
	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("error = %v, want ErrInvalidUsername", err)
	}
	if repository.user != nil {
		t.Fatal("repository should not receive an invalid username")
	}
}

func TestAccountServiceRegisterRejectsInvalidPassword(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.Register(context.Background(), "student@example.com", "student", "weakpass")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("error = %v, want ErrInvalidPassword", err)
	}
	if repository.user != nil {
		t.Fatal("repository should not receive an invalid password")
	}
}

func TestAccountServiceUpdateProfilePreservesEmptyPassword(t *testing.T) {
	uid := uuid.New()
	oldHash, err := hash.HashPassword("OldPass1")
	if err != nil {
		t.Fatal(err)
	}
	account := &User{UID: uid, Username: "old", PasswordHash: oldHash}
	repository := &fakeAccountRepository{user: account}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

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

func TestAccountServiceUpdateProfileRejectsInvalidUsername(t *testing.T) {
	uid := uuid.New()
	account := &User{UID: uid, Username: "old"}
	repository := &fakeAccountRepository{user: account}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.UpdateProfile(context.Background(), uid, "new_user", "", "")
	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("error = %v, want ErrInvalidUsername", err)
	}
	if repository.updated != nil {
		t.Fatal("repository should not persist an invalid username")
	}
}

func TestAccountServiceUpdateProfileRejectsInvalidPassword(t *testing.T) {
	uid := uuid.New()
	account := &User{UID: uid, Username: "old"}
	repository := &fakeAccountRepository{user: account}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.UpdateProfile(context.Background(), uid, "", "", "weakpass")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("error = %v, want ErrInvalidPassword", err)
	}
	if repository.updated != nil {
		t.Fatal("repository should not persist an invalid password")
	}
}

func TestAccountServiceUpdateStatusDelegatesRecordedTransitions(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)
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

func TestAccountServiceSuspensionInvalidatesBeforeUpdatingStatus(t *testing.T) {
	uid := uuid.New()
	timestamp := time.Date(2026, 9, 22, 5, 30, 12, 0, time.UTC)
	order := []string{}
	repository := &fakeAccountRepository{callOrder: &order}
	writer := &fakeSuspensionWriter{callOrder: &order}
	sessions := &fakeAccountSessionRepository{callOrder: &order}
	service := NewAccountService(repository, writer, sessions, 15*time.Minute)

	if err := service.UpdateAccountStatus(context.Background(), uid, AccountStatusSuspended, timestamp); err != nil {
		t.Fatal(err)
	}
	if got, want := order, []string{"redis", "sessions", "postgres"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
	if writer.uid != uid || !writer.at.Equal(timestamp) || writer.ttl != 15*time.Minute {
		t.Fatalf("suspension write = %#v, want account, timestamp, and TTL", writer)
	}
	if sessions.uid != uid || repository.status != AccountStatusSuspended {
		t.Fatalf("suspension state = session uid %s, status %q", sessions.uid, repository.status)
	}
}

func TestAccountServiceSuspensionStopsWhenRedisFails(t *testing.T) {
	order := []string{}
	repository := &fakeAccountRepository{callOrder: &order}
	writer := &fakeSuspensionWriter{callOrder: &order, err: errors.New("Redis unavailable")}
	sessions := &fakeAccountSessionRepository{callOrder: &order}
	service := NewAccountService(repository, writer, sessions, time.Minute)

	if err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatusSuspended, time.Now()); err == nil {
		t.Fatal("Redis failure was ignored")
	}
	if got, want := order, []string{"redis"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
}

func TestAccountServiceSuspensionStopsWhenSessionRevocationFails(t *testing.T) {
	order := []string{}
	repository := &fakeAccountRepository{callOrder: &order}
	writer := &fakeSuspensionWriter{callOrder: &order}
	sessions := &fakeAccountSessionRepository{callOrder: &order, err: errors.New("database unavailable")}
	service := NewAccountService(repository, writer, sessions, time.Minute)

	if err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatusSuspended, time.Now()); err == nil {
		t.Fatal("session revocation failure was ignored")
	}
	if got, want := order, []string{"redis", "sessions"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
}

func TestAccountServiceReactivationSkipsInvalidation(t *testing.T) {
	order := []string{}
	repository := &fakeAccountRepository{callOrder: &order}
	writer := &fakeSuspensionWriter{callOrder: &order}
	sessions := &fakeAccountSessionRepository{callOrder: &order}
	service := NewAccountService(repository, writer, sessions, time.Minute)

	if err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatusActive, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, want := order, []string{"postgres"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAccountServicePropagatesRepositoryErrors(t *testing.T) {
	failure := errors.New("database unavailable")
	repository := &fakeAccountRepository{lookupErr: failure}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.UpdateProfile(context.Background(), uuid.New(), "new", "", "")
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want repository error", err)
	}
}
