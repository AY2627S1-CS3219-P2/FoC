// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Unit tests for the admin audit log (D-106 to D-115): what each admin
//   write logs, that a refused or failed attempt is logged too, that a failed
//   audit write never hides the real error, and the purge loop.
// Author review: PENDING — <reviewer to complete>

package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAdmin = AuditContext{ActorID: "11111111-1111-4111-8111-111111111111", ActorRole: "ADMIN", CorrelationID: "req-1"}

func (f *fakeRepository) CreateAudited(ctx context.Context, s *Supplier, e *AuditEntry) error {
	if f.auditedErr != nil {
		return f.auditedErr
	}
	f.audits = append(f.audits, e)
	return f.Create(ctx, s)
}

func (f *fakeRepository) UpdateAudited(ctx context.Context, s *Supplier, e *AuditEntry) error {
	if f.auditedErr != nil {
		return f.auditedErr
	}
	f.audits = append(f.audits, e)
	return f.Update(ctx, s)
}

func (f *fakeRepository) SoftDeleteAudited(ctx context.Context, id string, e *AuditEntry) error {
	if f.auditedErr != nil {
		return f.auditedErr
	}
	f.audits = append(f.audits, e)
	return f.SoftDelete(ctx, id)
}

func (f *fakeRepository) RecordAudit(_ context.Context, e *AuditEntry) error {
	if f.recordAuditErr != nil {
		return f.recordAuditErr
	}
	f.audits = append(f.audits, e)
	return nil
}

func (f *fakeRepository) ListAudit(_ context.Context, flt AuditFilter) (*AuditPage, error) {
	return &AuditPage{Items: f.audits, Total: len(f.audits), Limit: flt.Limit, Offset: flt.Offset}, nil
}

func (f *fakeRepository) PurgeAudit(context.Context) (int64, error) {
	if f.purged != nil {
		f.purged <- struct{}{}
	}
	return 0, f.purgeErr
}

func auditSupplier() *Supplier {
	return &Supplier{Name: "Deck", Type: "Food", Building: "FASS", LocationDescription: "Canteen", IsAvailable: true}
}

func newAuditService(t *testing.T) (*Service, *fakeRepository, *fakeErrandCounter) {
	t.Helper()
	repo := newFakeRepository()
	errands := &fakeErrandCounter{}
	return NewService(repo, errands, discardLog()), repo, errands
}

func TestAdminCreateLogsWhatItSet(t *testing.T) {
	svc, repo, _ := newAuditService(t)

	created, err := svc.AdminCreate(context.Background(), testAdmin, auditSupplier())
	require.NoError(t, err)

	require.Len(t, repo.audits, 1)
	e := repo.audits[0]
	assert.Equal(t, ActionCreate, e.Action)
	assert.Equal(t, TargetType, e.TargetType)
	assert.Equal(t, created.ID, e.TargetID)
	assert.Equal(t, OutcomeSucceeded, e.Outcome)
	assert.Empty(t, e.RefusalReason)
	assert.Equal(t, testAdmin.ActorID, e.ActorID)
	assert.Equal(t, "ADMIN", e.ActorRole)
	assert.Equal(t, "req-1", e.CorrelationID)
	assert.Equal(t, "Deck", e.AdditionalComments["name"])
	assert.Nil(t, e.BeforeState, "a supplier action is not a status change")
}

func TestPlainCreateWritesNoAuditRow(t *testing.T) {
	svc, repo, _ := newAuditService(t)

	_, err := svc.Create(context.Background(), auditSupplier())

	require.NoError(t, err)
	assert.Empty(t, repo.audits, "the first-boot seed is nobody's admin action")
}

func TestRefusedCreateIsLoggedAndSavesNothing(t *testing.T) {
	cases := []struct {
		name       string
		prepare    func(repo *fakeRepository, in *Supplier)
		in         *Supplier
		wantReason string
	}{
		{name: "missing field", in: &Supplier{Name: "x"}, wantReason: "missing required field"},
		{name: "duplicate", in: auditSupplier(), prepare: func(repo *fakeRepository, in *Supplier) {
			repo.duplicates[in.Name+"|"+in.Building+"|"+in.LocationDescription] = true
		}, wantReason: ErrDuplicate.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := newAuditService(t)
			if tc.prepare != nil {
				tc.prepare(repo, tc.in)
			}

			_, err := svc.AdminCreate(context.Background(), testAdmin, tc.in)

			require.Error(t, err)
			assert.Empty(t, repo.byID, "nothing is saved")
			require.Len(t, repo.audits, 1)
			e := repo.audits[0]
			assert.Equal(t, ActionCreate, e.Action)
			assert.Equal(t, OutcomeRefused, e.Outcome)
			assert.Contains(t, e.RefusalReason, tc.wantReason)
			assert.Empty(t, e.TargetID, "a create that never got an id names no target")
			assert.Equal(t, testAdmin.ActorID, e.ActorID)
		})
	}
}

func TestAdminUpdateLogsOnlyTheFieldsThatChanged(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	created, err := svc.Create(context.Background(), auditSupplier())
	require.NoError(t, err)

	edit := auditSupplier()
	edit.Name = "The Deck"
	edit.OpeningTime = "08:00"
	_, err = svc.AdminUpdate(context.Background(), testAdmin, created.ID, edit)
	require.NoError(t, err)

	require.Len(t, repo.audits, 1)
	e := repo.audits[0]
	assert.Equal(t, ActionUpdate, e.Action)
	assert.Equal(t, created.ID, e.TargetID)
	assert.Equal(t, OutcomeSucceeded, e.Outcome)
	assert.Equal(t, map[string]any{"name": "The Deck", "opening_time": "08:00"}, e.AdditionalComments)
}

func TestAdminUpdateWithNoChangeStillLogsAnEmptyChange(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	created, err := svc.Create(context.Background(), auditSupplier())
	require.NoError(t, err)

	_, err = svc.AdminUpdate(context.Background(), testAdmin, created.ID, auditSupplier())
	require.NoError(t, err)

	require.Len(t, repo.audits, 1)
	assert.Equal(t, map[string]any{}, repo.audits[0].AdditionalComments)
}

func TestRefusedUpdateOfAnUnknownSupplierIsLogged(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	id := "22222222-2222-4222-8222-222222222222"

	_, err := svc.AdminUpdate(context.Background(), testAdmin, id, auditSupplier())

	require.ErrorIs(t, err, ErrNotFound)
	require.Len(t, repo.audits, 1)
	assert.Equal(t, OutcomeRefused, repo.audits[0].Outcome)
	assert.Equal(t, id, repo.audits[0].TargetID)
	assert.Equal(t, ErrNotFound.Error(), repo.audits[0].RefusalReason)
}

func TestAttemptOnAMalformedIdIsLoggedAsTyped(t *testing.T) {
	svc, repo, _ := newAuditService(t)

	err := svc.AdminDelete(context.Background(), testAdmin, "not-a-uuid")

	require.ErrorIs(t, err, ErrNotFound)
	require.Len(t, repo.audits, 1)
	assert.Equal(t, "not-a-uuid", repo.audits[0].TargetID)
}

func TestAdminDeleteLogsWhatWasDeleted(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	created, err := svc.Create(context.Background(), auditSupplier())
	require.NoError(t, err)

	require.NoError(t, svc.AdminDelete(context.Background(), testAdmin, created.ID))

	require.Len(t, repo.audits, 1)
	e := repo.audits[0]
	assert.Equal(t, ActionDelete, e.Action)
	assert.Equal(t, created.ID, e.TargetID)
	assert.Equal(t, OutcomeSucceeded, e.Outcome)
	assert.Equal(t, map[string]any{"name": "Deck", "building": "FASS"}, e.AdditionalComments)
}

func TestDeleteRefusedForAnUnfinishedErrandIsLogged(t *testing.T) {
	svc, repo, errands := newAuditService(t)
	created, err := svc.Create(context.Background(), auditSupplier())
	require.NoError(t, err)
	errands.count = 2

	err = svc.AdminDelete(context.Background(), testAdmin, created.ID)

	require.ErrorIs(t, err, ErrInUse)
	assert.Contains(t, repo.byID, created.ID, "the supplier is still there")
	require.Len(t, repo.audits, 1)
	assert.Equal(t, OutcomeRefused, repo.audits[0].Outcome)
	assert.Equal(t, ErrInUse.Error(), repo.audits[0].RefusalReason)
}

func TestDeleteThatCannotCountErrandsIsLoggedAsFailed(t *testing.T) {
	svc, repo, errands := newAuditService(t)
	created, err := svc.Create(context.Background(), auditSupplier())
	require.NoError(t, err)
	errands.err = errors.New("order-service unreachable")

	err = svc.AdminDelete(context.Background(), testAdmin, created.ID)

	require.Error(t, err)
	require.Len(t, repo.audits, 1)
	assert.Equal(t, OutcomeFailed, repo.audits[0].Outcome)
	assert.Contains(t, repo.audits[0].RefusalReason, "order-service unreachable")
}

func TestFailedAuditedWriteSavesNothingAndIsLoggedAsFailed(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	repo.auditedErr = errors.New("connection reset")

	_, err := svc.AdminCreate(context.Background(), testAdmin, auditSupplier())

	require.Error(t, err)
	assert.Empty(t, repo.byID, "the supplier and its row are saved together or not at all (D-108)")
	require.Len(t, repo.audits, 1, "only the attempt row is there")
	assert.Equal(t, OutcomeFailed, repo.audits[0].Outcome)
}

func TestAFailedAttemptRowNeverHidesTheRealError(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	repo.recordAuditErr = errors.New("audit table gone")

	_, err := svc.AdminCreate(context.Background(), testAdmin, &Supplier{})

	require.ErrorIs(t, err, ErrValidation, "the caller still gets the refusal")
	assert.Empty(t, repo.audits)
}

func TestAnAuditRowThatCannotBeWrittenIsLoggedWithTheRequestID(t *testing.T) {
	var logs bytes.Buffer
	repo := newFakeRepository()
	repo.recordAuditErr = errors.New("audit table gone")
	svc := NewService(repo, &fakeErrandCounter{}, slog.New(slog.NewJSONHandler(&logs, nil)))

	_, err := svc.AdminCreate(context.Background(), testAdmin, &Supplier{})

	require.ErrorIs(t, err, ErrValidation)
	var line map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &line), "exactly one JSON line: %s", logs.String())
	assert.Equal(t, "ERROR", line["level"])
	assert.Equal(t, testAdmin.CorrelationID, line["request_id"])
	assert.Equal(t, testAdmin.ActorID, line["user_id"])
	assert.Equal(t, ActionCreate, line["action"])
	assert.Equal(t, OutcomeRefused, line["outcome"])
	assert.Contains(t, line["err"], "audit table gone")
}

func TestLongFailureTextIsCut(t *testing.T) {
	outcome, reason := classify(errors.New(string(make([]byte, 1000))))

	assert.Equal(t, OutcomeFailed, outcome)
	assert.Len(t, reason, maxReasonLen)
}

func TestAuditLogPagesAtFiftyAndChecksItsFilters(t *testing.T) {
	svc, _, _ := newAuditService(t)
	ctx := context.Background()

	for _, limit := range []int{0, -3, 50, 51, 500} {
		page, err := svc.AuditLog(ctx, AuditFilter{Limit: limit})
		require.NoError(t, err)
		assert.Equal(t, MaxAuditPage, page.Limit, "limit %d", limit)
	}
	page, err := svc.AuditLog(ctx, AuditFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 10, page.Limit)

	_, err = svc.AuditLog(ctx, AuditFilter{Offset: -1})
	assert.ErrorIs(t, err, ErrValidation)
	_, err = svc.AuditLog(ctx, AuditFilter{Outcome: "maybe"})
	assert.ErrorIs(t, err, ErrValidation)
	for _, ok := range []string{OutcomeSucceeded, OutcomeRefused, OutcomeFailed, ""} {
		_, err = svc.AuditLog(ctx, AuditFilter{Outcome: ok})
		assert.NoError(t, err, "outcome %q", ok)
	}
}

func TestPurgeRunsAtStartAndOnEveryTickUntilCancelled(t *testing.T) {
	svc, repo, _ := newAuditService(t)
	repo.purged = make(chan struct{}, 16)
	repo.purgeErr = errors.New("one bad run does not stop the loop")
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
