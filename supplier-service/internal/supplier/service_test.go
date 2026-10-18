package supplier

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	byID       map[string]*Supplier
	duplicates map[string]bool
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		byID:       map[string]*Supplier{},
		duplicates: map[string]bool{},
	}
}

func (f *fakeRepository) Create(_ context.Context, s *Supplier) error {
	f.byID[s.ID] = s
	return nil
}

func (f *fakeRepository) GetByID(_ context.Context, id string) (*Supplier, error) {
	return f.byID[id], nil
}

func (f *fakeRepository) List(_ context.Context, _ ListFilter) ([]*Supplier, error) {
	var out []*Supplier
	for _, s := range f.byID {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeRepository) Update(_ context.Context, s *Supplier) error {
	f.byID[s.ID] = s
	return nil
}

func (f *fakeRepository) SoftDelete(_ context.Context, id string) error {
	if s, ok := f.byID[id]; ok {
		now := s.UpdatedAt
		s.DeletedAt = &now
		s.IsAvailable = false
		delete(f.byID, id)
	}
	return nil
}

func (f *fakeRepository) ExistsDuplicate(_ context.Context, name, building, locationDescription, excludeID string) (bool, error) {
	return f.duplicates[name+"|"+building+"|"+locationDescription], nil
}

func (f *fakeRepository) Count(_ context.Context) (int, error) {
	return len(f.byID), nil
}

// AI-generated (edited by James Teo).
// fakeErrandCounter answers count and err, and records what it was asked.
// Its zero value reports no unfinished errands.
type fakeErrandCounter struct {
	count int
	err   error

	calls  int
	gotID  string
	gotCtx context.Context
}

// AI-generated (edited by James Teo).
func (f *fakeErrandCounter) ActiveErrands(ctx context.Context, supplierID string) (int, error) {
	f.calls++
	f.gotID = supplierID
	f.gotCtx = ctx
	return f.count, f.err
}

func validSupplier() *Supplier {
	return &Supplier{
		Name:                "Cool Spot",
		Type:                "Food",
		Building:            "Com2",
		LocationDescription: "Opp LT16",
		Latitude:            1.29,
		Longitude:           103.77,
		OpeningTime:         "09:00",
		ClosingTime:         "21:30",
	}
}

func TestCreate_Success(t *testing.T) {
	repo := newFakeRepository()
	// AI-generated (edited by James Teo).
	svc := NewService(repo, &fakeErrandCounter{})

	created, err := svc.Create(context.Background(), validSupplier())

	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.True(t, created.IsAvailable)
}

func TestCreate_MissingRequiredField(t *testing.T) {
	repo := newFakeRepository()
	// AI-generated (edited by James Teo).
	svc := NewService(repo, &fakeErrandCounter{})

	in := validSupplier()
	in.Name = ""

	_, err := svc.Create(context.Background(), in)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrValidation))
}

func TestCreate_InvalidLatitude(t *testing.T) {
	repo := newFakeRepository()
	// AI-generated (edited by James Teo).
	svc := NewService(repo, &fakeErrandCounter{})

	in := validSupplier()
	in.Latitude = 200

	_, err := svc.Create(context.Background(), in)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrValidation))
}

func TestCreate_DuplicateRejected(t *testing.T) {
	repo := newFakeRepository()
	repo.duplicates["Cool Spot|Com2|Opp LT16"] = true
	// AI-generated (edited by James Teo).
	svc := NewService(repo, &fakeErrandCounter{})

	_, err := svc.Create(context.Background(), validSupplier())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicate))
}

func TestGet_NotFound(t *testing.T) {
	repo := newFakeRepository()
	// AI-generated (edited by James Teo).
	svc := NewService(repo, &fakeErrandCounter{})

	_, err := svc.Get(context.Background(), "missing-id")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestDelete_SoftDeletesAndRemovesFromActiveSet(t *testing.T) {
	repo := newFakeRepository()
	// AI-generated (edited by James Teo).
	svc := NewService(repo, &fakeErrandCounter{})
	created, err := svc.Create(context.Background(), validSupplier())
	require.NoError(t, err)

	err = svc.Delete(context.Background(), created.ID)
	require.NoError(t, err)

	_, err = svc.Get(context.Background(), created.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "soft-deleted supplier should no longer be gettable")
}

func TestDelete_NotFound(t *testing.T) {
	repo := newFakeRepository()
	// AI-generated (edited by James Teo).
	errands := &fakeErrandCounter{}
	svc := NewService(repo, errands)

	err := svc.Delete(context.Background(), "missing-id")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
	// AI-generated (edited by James Teo).
	assert.Zero(t, errands.calls, "a missing supplier needs no errand check")
}

// AI-generated (edited by James Teo).
func TestDelete_ChecksForUnfinishedErrands(t *testing.T) {
	errOrderService := errors.New("order-service did not answer")

	tests := []struct {
		name        string
		count       int
		countErr    error
		wantErr     error // nil: the delete goes through
		wantDeleted bool
	}{
		{name: "no unfinished errands: soft-deleted", count: 0, wantDeleted: true},
		{name: "one unfinished errand: refused", count: 1, wantErr: ErrInUse},
		{name: "several unfinished errands: refused", count: 4, wantErr: ErrInUse},
		{name: "count unavailable: refused", countErr: errOrderService, wantErr: errOrderService},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			errands := &fakeErrandCounter{count: tc.count, err: tc.countErr}
			svc := NewService(repo, errands)
			created, err := svc.Create(context.Background(), validSupplier())
			require.NoError(t, err)

			err = svc.Delete(context.Background(), created.ID)

			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr))
			}
			if tc.countErr != nil {
				assert.False(t, errors.Is(err, ErrInUse), "an unanswered check is a failure, not a conflict")
			}
			assert.Equal(t, 1, errands.calls)

			_, getErr := svc.Get(context.Background(), created.ID)
			if tc.wantDeleted {
				assert.True(t, errors.Is(getErr, ErrNotFound), "soft-deleted supplier should no longer be gettable")
			} else {
				require.NoError(t, getErr, "a refused delete must leave the supplier in place")
				assert.Nil(t, repo.byID[created.ID].DeletedAt)
				assert.True(t, repo.byID[created.ID].IsAvailable)
			}
		})
	}
}

// AI-generated (edited by James Teo).
// ctxMarker tags a context so a test can tell whether it reached the counter.
type ctxMarker struct{}

// AI-generated (edited by James Teo).
func TestDelete_GivesTheCounterTheCallersContextAndID(t *testing.T) {
	repo := newFakeRepository()
	errands := &fakeErrandCounter{}
	svc := NewService(repo, errands)
	created, err := svc.Create(context.Background(), validSupplier())
	require.NoError(t, err)

	// The counter's adapter reads the admin's token from the context, so the
	// counter must get the caller's context, not a fresh one.
	ctx := context.WithValue(context.Background(), ctxMarker{}, "Bearer admin-token")
	require.NoError(t, svc.Delete(ctx, created.ID))

	assert.Equal(t, created.ID, errands.gotID)
	require.NotNil(t, errands.gotCtx)
	assert.Equal(t, "Bearer admin-token", errands.gotCtx.Value(ctxMarker{}))
}
