// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for registration, profile updates, and account-status transitions.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/hash"

	"github.com/google/uuid"
)

func TestAccountServiceRegisterHashesPasswordAndSetsDefaults(t *testing.T) {
	repository := &fakeAccountRepository{user: &User{}}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	account, err := service.Register(context.Background(), "student@u.nus.edu", "student", "ValidPass1")
	if err != nil {
		t.Fatal(err)
	}
	if repository.user != account {
		t.Fatalf("created account pointer = %p, want %p", repository.user, account)
	}
	if account.Email != "student@u.nus.edu" || account.Username != "student" || account.AccountRole != AccountRoleStudent || account.AccountStatus != AccountStatusActive {
		t.Fatalf("created account = %#v", account)
	}
	if account.PasswordHash == "ValidPass1" || !hash.CheckPassword(account.PasswordHash, "ValidPass1") {
		t.Fatal("registration password was not stored as a valid hash")
	}
}

// AI-generated (edited by PENDING): NUS email validation is enforced by the domain service for every caller.
func TestAccountServiceRegisterRejectsNonNUSEmail(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.Register(context.Background(), "student@example.com", "student", "ValidPass1")
	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("error = %v, want ErrInvalidEmail", err)
	}
	if repository.user != nil {
		t.Fatal("repository should not persist a non-NUS email")
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

	_, err := service.Register(context.Background(), "student@u.nus.edu", "student_user", "ValidPass1")
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

	_, err := service.Register(context.Background(), "student@u.nus.edu", "student", "weakpass")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("error = %v, want ErrInvalidPassword", err)
	}
	if repository.user != nil {
		t.Fatal("repository should not receive an invalid password")
	}
}

// AI-generated (edited by ZI YANG): database-width validation belongs before persistence.
func TestAccountServiceRegisterRejectsEmailLongerThanDatabaseWidth(t *testing.T) {
	repository := &fakeAccountRepository{}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)
	email := strings.Repeat("a", 246) + "@u.nus.edu"

	_, err := service.Register(context.Background(), email, "student", "ValidPass1")
	if !errors.Is(err, ErrEmailTooLong) {
		t.Fatalf("error = %v, want ErrEmailTooLong", err)
	}
	if repository.user != nil {
		t.Fatal("repository should not persist an overlong email")
	}
}

// AI-generated (edited by PENDING): profile reads cross the domain boundary before reaching persistence.
func TestAccountServiceGetByIDReturnsRepositoryAccount(t *testing.T) {
	uid := uuid.New()
	want := &User{UID: uid, Username: "student"}
	service := NewAccountService(&fakeAccountRepository{user: want}, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	got, err := service.GetByID(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("account = %p, want %p", got, want)
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

// AI-generated (edited by ZI YANG): database-width validation belongs before persistence.
func TestAccountServiceUpdateProfileRejectsPhoneLongerThanDatabaseWidth(t *testing.T) {
	uid := uuid.New()
	repository := &fakeAccountRepository{user: &User{UID: uid, PhoneNum: "91234567"}}
	service := NewAccountService(repository, &fakeSuspensionWriter{}, &fakeAccountSessionRepository{}, time.Minute)

	_, err := service.UpdateProfile(context.Background(), uid, "", strings.Repeat("1", 21), "")
	if !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("error = %v, want ErrInvalidPhone", err)
	}
	if repository.updated != nil {
		t.Fatal("repository should not persist an overlong phone number")
	}
}

func TestAccountServiceUpdateStatusDelegatesRecordedTransitions(t *testing.T) {
	repository := &fakeAccountRepository{user: &User{}}
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
	repository := &fakeAccountRepository{user: &User{}, callOrder: &order}
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
	repository := &fakeAccountRepository{user: &User{}, callOrder: &order}
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
	repository := &fakeAccountRepository{user: &User{}, callOrder: &order}
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

// AI-generated (edited by ZI YANG).
func TestAccountServiceDoesNotInvalidateUnknownSuspension(t *testing.T) {
	order := []string{}
	repository := &fakeAccountRepository{lookupErr: ErrNotFound, callOrder: &order}
	writer := &fakeSuspensionWriter{callOrder: &order}
	sessions := &fakeAccountSessionRepository{callOrder: &order}
	service := NewAccountService(repository, writer, sessions, time.Minute)

	err := service.UpdateAccountStatus(context.Background(), uuid.New(), AccountStatusSuspended, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if len(order) != 0 {
		t.Fatalf("side-effect order = %#v, want no Redis, session, or PostgreSQL calls", order)
	}
}

// AI-generated (edited by ZI YANG).
func TestAccountServiceSuspensionRetryRepeatsInvalidationBeforePostgres(t *testing.T) {
	uid := uuid.New()
	timestamp := time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC)
	order := []string{}
	repository := &fakeAccountRepository{
		user:       &User{},
		callOrder:  &order,
		statusErrs: []error{errors.New("database unavailable"), nil},
	}
	writer := &fakeSuspensionWriter{callOrder: &order}
	sessions := &fakeAccountSessionRepository{callOrder: &order}
	service := NewAccountService(repository, writer, sessions, 15*time.Minute)

	if err := service.UpdateAccountStatus(context.Background(), uid, AccountStatusSuspended, timestamp); err == nil {
		t.Fatal("PostgreSQL failure was ignored")
	}
	if err := service.UpdateAccountStatus(context.Background(), uid, AccountStatusSuspended, timestamp); err != nil {
		t.Fatalf("retry suspension: %v", err)
	}

	if got, want := order, []string{"redis", "sessions", "postgres", "redis", "sessions", "postgres"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
	if writer.uid != uid || !writer.at.Equal(timestamp) || writer.ttl != 15*time.Minute {
		t.Fatalf("retried suspension write = %#v, want identical account, timestamp, and TTL", writer)
	}
}

func TestAccountServiceReactivationSkipsInvalidation(t *testing.T) {
	order := []string{}
	repository := &fakeAccountRepository{user: &User{}, callOrder: &order}
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
