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
	svc := NewService(repo)

	created, err := svc.Create(context.Background(), validSupplier())

	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.True(t, created.IsAvailable)
}

func TestCreate_MissingRequiredField(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	in := validSupplier()
	in.Name = ""

	_, err := svc.Create(context.Background(), in)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrValidation))
}

func TestCreate_InvalidLatitude(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	in := validSupplier()
	in.Latitude = 200

	_, err := svc.Create(context.Background(), in)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrValidation))
}

func TestCreate_DuplicateRejected(t *testing.T) {
	repo := newFakeRepository()
	repo.duplicates["Cool Spot|Com2|Opp LT16"] = true
	svc := NewService(repo)

	_, err := svc.Create(context.Background(), validSupplier())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicate))
}

func TestGet_NotFound(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	_, err := svc.Get(context.Background(), "missing-id")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestDelete_SoftDeletesAndRemovesFromActiveSet(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)
	created, err := svc.Create(context.Background(), validSupplier())
	require.NoError(t, err)

	err = svc.Delete(context.Background(), created.ID)
	require.NoError(t, err)

	_, err = svc.Get(context.Background(), created.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "soft-deleted supplier should no longer be gettable")
}

func TestDelete_NotFound(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	err := svc.Delete(context.Background(), "missing-id")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}
