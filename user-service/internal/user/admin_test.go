// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Unit tests for the admin side of AccountService (D-106 to D-123): what
//   each admin action logs, that no admin is suspended, that a refused or failed
//   attempt is logged, that a password never reaches the log, the account
//   search's checks, and the purge loop.
// Author review: PENDING — <reviewer to complete>

package user

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var testAdminCtx = AuditContext{ActorID: "11111111-1111-4111-8111-111111111111", ActorRole: "ADMIN", CorrelationID: "req-1"}

func (f *fakeAccountRepository) UpdateAccountStatusAudited(ctx context.Context, uid uuid.UUID, status AccountStatus, tva time.Time, e *AuditEntry) error {
	if f.auditedErr != nil {
		return f.auditedErr
	}
	f.auditedStatus = e
	return f.UpdateAccountStatusByID(ctx, uid, status, tva)
}

func (f *fakeAccountRepository) UpdateAsAdminAudited(ctx context.Context, u *User, entries []*AuditEntry) error {
	if f.auditedErr != nil {
		return f.auditedErr
	}
	f.adminEntries = entries
	return f.Update(ctx, u)
}

func (f *fakeAccountRepository) RecordAudit(_ context.Context, e *AuditEntry) error {
	if f.recordAuditErr != nil {
		return f.recordAuditErr
	}
	f.audits = append(f.audits, e)
	return nil
}

func (f *fakeAccountRepository) ListAudit(_ context.Context, flt AuditFilter) (*AuditPage, error) {
	return &AuditPage{Items: f.audits, Total: len(f.audits), Limit: flt.Limit, Offset: flt.Offset}, nil
}

func (f *fakeAccountRepository) PurgeAudit(context.Context) (int64, error) {
	if f.purged != nil {
		f.purged <- struct{}{}
	}
	return 0, f.purgeErr
}

func (f *fakeAccountRepository) SearchUsers(_ context.Context, flt UserFilter) (*UserPage, error) {
	f.searched = flt
	if f.searchPage != nil {
		return f.searchPage, nil
	}
	return &UserPage{Items: []*User{}, Limit: flt.Limit, Offset: flt.Offset}, nil
}

func newAdminService(repo *fakeAccountRepository) (*AccountService, *fakeSuspensionWriter, *fakeAccountSessionRepository) {
	writer := &fakeSuspensionWriter{}
	sessions := &fakeAccountSessionRepository{}
	return NewAccountService(repo, writer, sessions, time.Minute), writer, sessions
}

func student(uid uuid.UUID) *User {
	return &User{UID: uid, Username: "alice", PhoneNum: "91234567", AccountRole: AccountRoleStudent, AccountStatus: AccountStatusActive}
}

func TestAdminSuspendAndReactivateAreLoggedWithTheirAction(t *testing.T) {
	for _, tc := range []struct {
		status AccountStatus
		action string
	}{{AccountStatusSuspended, ActionSuspend}, {AccountStatusActive, ActionReactivate}} {
		uid := uuid.New()
		repo := &fakeAccountRepository{user: student(uid)}
		svc, _, _ := newAdminService(repo)

		if err := svc.AdminUpdateAccountStatus(context.Background(), testAdminCtx, uid, tc.status); err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}

		e := repo.auditedStatus
		if e == nil {
			t.Fatalf("%s: no audit row was saved with the change", tc.action)
		}
		if e.Action != tc.action || e.TargetType != AuditTargetType || e.TargetID != uid.String() || e.Outcome != OutcomeSucceeded {
			t.Errorf("%s: row = %+v", tc.action, e)
		}
		if e.ActorID != testAdminCtx.ActorID || e.ActorRole != "ADMIN" || e.CorrelationID != "req-1" {
			t.Errorf("%s: wrong attribution: %+v", tc.action, e)
		}
		if len(repo.audits) != 0 {
			t.Errorf("%s: a succeeded action needs no separate attempt row", tc.action)
		}
	}
}

func TestNoAdminCanBeSuspendedByAnotherAdminOrByThemselves(t *testing.T) {
	other := uuid.New()
	self := uuid.MustParse(testAdminCtx.ActorID)
	for _, tc := range []struct {
		name string
		uid  uuid.UUID
		want error
	}{{"another admin", other, ErrCannotSuspendAdmin}, {"themselves", self, ErrCannotSuspendSelf}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeAccountRepository{
				user:         &User{UID: tc.uid, AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive},
				activeAdmins: 5,
			}
			svc, writer, sessions := newAdminService(repo)

			err := svc.AdminUpdateAccountStatus(context.Background(), testAdminCtx, tc.uid, AccountStatusSuspended)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if writer.uid != uuid.Nil || sessions.uid != uuid.Nil {
				t.Error("a refused suspension must not touch Redis or the sessions")
			}
			if repo.auditedStatus != nil {
				t.Error("nothing is saved for a refused suspension")
			}
			if len(repo.audits) != 1 || repo.audits[0].Outcome != OutcomeRefused || repo.audits[0].Action != ActionSuspend {
				t.Fatalf("the refusal must be logged (D-109): %+v", repo.audits)
			}
			if repo.audits[0].RefusalReason != tc.want.Error() || repo.audits[0].TargetID != tc.uid.String() {
				t.Errorf("row = %+v", repo.audits[0])
			}
		})
	}
}

func TestTheOldUnauditedStatusCallStillWorksForSeedAndTests(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{user: &User{UID: uid, AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive}, activeAdmins: 2}
	svc, _, _ := newAdminService(repo)

	if err := svc.UpdateAccountStatus(context.Background(), uid, AccountStatusSuspended); err != nil {
		t.Fatalf("the unaudited path keeps its old rules: %v", err)
	}
	if repo.auditedStatus != nil || len(repo.audits) != 0 {
		t.Error("the unaudited path writes no audit row")
	}
}

func TestRefusedAndFailedStatusAttemptsAreLogged(t *testing.T) {
	uid := uuid.New()
	for _, tc := range []struct {
		name        string
		repo        *fakeAccountRepository
		wantOutcome string
	}{
		{"unverified", &fakeAccountRepository{user: &User{UID: uid, AccountRole: AccountRoleStudent, AccountStatus: AccountStatusUnverified}}, OutcomeRefused},
		{"unknown account", &fakeAccountRepository{lookupErr: ErrNotFound}, OutcomeRefused},
		{"the saved change fails", &fakeAccountRepository{user: student(uid), auditedErr: errors.New("connection reset")}, OutcomeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newAdminService(tc.repo)

			err := svc.AdminUpdateAccountStatus(context.Background(), testAdminCtx, uid, AccountStatusSuspended)

			if err == nil {
				t.Fatal("expected an error")
			}
			if len(tc.repo.audits) != 1 || tc.repo.audits[0].Outcome != tc.wantOutcome {
				t.Fatalf("attempt rows = %+v, want one %s", tc.repo.audits, tc.wantOutcome)
			}
		})
	}
}

func TestAFailedAttemptRowNeverHidesTheRealError(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{
		user:           &User{UID: uid, AccountRole: AccountRoleAdmin, AccountStatus: AccountStatusActive},
		recordAuditErr: errors.New("audit table gone"),
	}
	svc, _, _ := newAdminService(repo)

	err := svc.AdminUpdateAccountStatus(context.Background(), testAdminCtx, uid, AccountStatusSuspended)

	if !errors.Is(err, ErrCannotSuspendAdmin) {
		t.Fatalf("the caller must still get the refusal, got %v", err)
	}
}

func TestAPasswordResetIsLoggedAsAChangeWithNoValue(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{user: student(uid)}
	svc, _, sessions := newAdminService(repo)
	const password = "BrandNewPass9"

	if _, err := svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "", "", password); err != nil {
		t.Fatal(err)
	}

	if len(repo.adminEntries) != 1 {
		t.Fatalf("entries = %d, want one password reset row", len(repo.adminEntries))
	}
	e := repo.adminEntries[0]
	if e.Action != ActionPasswordReset || e.Outcome != OutcomeSucceeded || e.TargetID != uid.String() {
		t.Errorf("row = %+v", e)
	}
	if e.BeforeState != nil || e.AfterState != nil || e.AdditionalComments != nil {
		t.Errorf("a password reset holds no value (D-111): %+v", e)
	}
	raw, _ := json.Marshal(repo.adminEntries)
	if strings.Contains(string(raw), password) || strings.Contains(string(raw), repo.updated.PasswordHash) {
		t.Error("neither the password nor its hash may reach the audit log")
	}
	if sessions.uid != uid {
		t.Error("a reset still revokes the account's sessions")
	}
}

func TestAProfileEditLogsTheNewValuesOfWhatChanged(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{user: student(uid)}
	svc, _, _ := newAdminService(repo)

	if _, err := svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "alice2", "91234567", ""); err != nil {
		t.Fatal(err)
	}

	if len(repo.adminEntries) != 1 || repo.adminEntries[0].Action != ActionUpdate {
		t.Fatalf("entries = %+v", repo.adminEntries)
	}
	got := repo.adminEntries[0].AdditionalComments
	if len(got) != 1 || got["username"] != "alice2" {
		t.Errorf("comments = %v, want only the changed username (the phone is the same)", got)
	}
}

func TestAResetPlusAnEditLogsBothInOneTransaction(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{user: student(uid)}
	svc, _, _ := newAdminService(repo)

	if _, err := svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "alice2", "", "BrandNewPass9"); err != nil {
		t.Fatal(err)
	}

	if len(repo.adminEntries) != 2 || repo.adminEntries[0].Action != ActionPasswordReset || repo.adminEntries[1].Action != ActionUpdate {
		t.Fatalf("entries = %+v", repo.adminEntries)
	}
}

func TestAnEditThatChangesNothingStillLogsAnEmptyChange(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{user: student(uid)}
	svc, _, _ := newAdminService(repo)

	if _, err := svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "", "", ""); err != nil {
		t.Fatal(err)
	}

	if len(repo.adminEntries) != 1 || repo.adminEntries[0].Action != ActionUpdate || len(repo.adminEntries[0].AdditionalComments) != 0 {
		t.Fatalf("entries = %+v", repo.adminEntries)
	}
}

func TestARefusedEditIsLoggedWithoutThePassword(t *testing.T) {
	uid := uuid.New()
	repo := &fakeAccountRepository{user: student(uid)}
	svc, _, _ := newAdminService(repo)
	const password = "BrandNewPass9"

	_, err := svc.AdminUpdateProfile(context.Background(), testAdminCtx, uid, "bad name!", "", password)

	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("error = %v", err)
	}
	if len(repo.audits) != 1 || repo.audits[0].Outcome != OutcomeRefused || repo.audits[0].Action != ActionPasswordReset {
		t.Fatalf("rows = %+v", repo.audits)
	}
	if raw, _ := json.Marshal(repo.audits); strings.Contains(string(raw), password) {
		t.Error("the password must never reach the log, even for a refused attempt")
	}
	if repo.audits[0].AdditionalComments["username"] != "bad name!" {
		t.Error("the attempted username is kept")
	}
}

func TestSearchUsersChecksAndClampsItsFilter(t *testing.T) {
	repo := &fakeAccountRepository{}
	svc, _, _ := newAdminService(repo)
	ctx := context.Background()

	for _, limit := range []int{0, -1, 50, 51, 1000} {
		if _, err := svc.SearchUsers(ctx, UserFilter{Limit: limit}); err != nil {
			t.Fatal(err)
		}
		if repo.searched.Limit != MaxPage {
			t.Errorf("limit %d reached the repository as %d, want %d", limit, repo.searched.Limit, MaxPage)
		}
	}
	if _, err := svc.SearchUsers(ctx, UserFilter{Limit: 10}); err != nil || repo.searched.Limit != 10 {
		t.Errorf("a limit under 50 is kept: %v, %d", err, repo.searched.Limit)
	}
	if _, err := svc.SearchUsers(ctx, UserFilter{Query: "  ali  "}); err != nil || repo.searched.Query != "ali" {
		t.Errorf("the search text is trimmed: %v, %q", err, repo.searched.Query)
	}
	for name, f := range map[string]UserFilter{
		"negative offset": {Offset: -1},
		"unknown status":  {Status: "BANNED"},
		"huge query":      {Query: strings.Repeat("a", 256)},
	} {
		if _, err := svc.SearchUsers(ctx, f); !errors.Is(err, ErrInvalidFilter) {
			t.Errorf("%s: error = %v, want ErrInvalidFilter", name, err)
		}
	}
	for _, s := range []AccountStatus{"", AccountStatusActive, AccountStatusSuspended, AccountStatusUnverified} {
		if _, err := svc.SearchUsers(ctx, UserFilter{Status: s}); err != nil {
			t.Errorf("status %q: %v", s, err)
		}
	}
}

func TestAuditLogChecksItsFilterAndPagesAtFifty(t *testing.T) {
	repo := &fakeAccountRepository{}
	svc, _, _ := newAdminService(repo)
	ctx := context.Background()

	page, err := svc.AuditLog(ctx, AuditFilter{Limit: 500})
	if err != nil || page.Limit != MaxPage {
		t.Fatalf("limit 500 -> %v, %v", page, err)
	}
	if _, err := svc.AuditLog(ctx, AuditFilter{Offset: -1}); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("negative offset: %v", err)
	}
	if _, err := svc.AuditLog(ctx, AuditFilter{Outcome: "maybe"}); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("unknown outcome: %v", err)
	}
	for _, o := range []string{"", OutcomeSucceeded, OutcomeRefused, OutcomeFailed} {
		if _, err := svc.AuditLog(ctx, AuditFilter{Outcome: o}); err != nil {
			t.Errorf("outcome %q: %v", o, err)
		}
	}
}

func TestLongFailureTextIsCut(t *testing.T) {
	outcome, reason := classify(errors.New(strings.Repeat("x", 1000)))
	if outcome != OutcomeFailed || len(reason) != maxReasonLen {
		t.Fatalf("outcome %q, reason length %d", outcome, len(reason))
	}
}

func TestPurgeRunsAtStartAndOnEveryTickUntilCancelled(t *testing.T) {
	repo := &fakeAccountRepository{purged: make(chan struct{}, 16), purgeErr: errors.New("one bad run does not stop the loop")}
	svc, _, _ := newAdminService(repo)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		svc.RunAuditPurge(ctx, 5*time.Millisecond)
		close(done)
	}()

	for i := 0; i < 3; i++ {
		select {
		case <-repo.purged:
		case <-time.After(5 * time.Second):
			t.Fatalf("purge run %d never happened", i+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop when its context was cancelled")
	}
}
