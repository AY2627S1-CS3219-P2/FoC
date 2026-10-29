// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Unit tests for the ranking-model service (D-095, D-099).
// Author review: PENDING — <reviewer to complete>

package ranking_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"foc/user-service/internal/ranking"

	"github.com/google/uuid"
)

type fakeStore struct {
	models map[uuid.UUID]ranking.Model
	err    error
}

func newFakeStore() *fakeStore { return &fakeStore{models: map[uuid.UUID]ranking.Model{}} }

func (f *fakeStore) Get(_ context.Context, id uuid.UUID) (ranking.Model, error) {
	if f.err != nil {
		return ranking.Model{}, f.err
	}
	m, ok := f.models[id]
	if !ok {
		return ranking.Model{}, ranking.ErrNotFound
	}
	return m, nil
}

func (f *fakeStore) Put(_ context.Context, m ranking.Model) error {
	if f.err != nil {
		return f.err
	}
	f.models[m.UserID] = m
	return nil
}

var now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func TestAUserSavesAndReadsTheirOwnModel(t *testing.T) {
	store := newFakeStore()
	svc := ranking.NewService(store, func() time.Time { return now })
	me := uuid.New()
	trained := now.Add(-time.Minute)

	saved, err := svc.Put(context.Background(), me, me, json.RawMessage(` {"version":1} `), trained)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !saved.UpdatedAt.Equal(now) || !saved.TrainedAt.Equal(trained) {
		t.Fatalf("saved times = %v, %v", saved.TrainedAt, saved.UpdatedAt)
	}

	got, err := svc.Get(context.Background(), me, me)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Data) != ` {"version":1} ` {
		t.Fatalf("Data = %s, kept as sent", got.Data)
	}
}

func TestRefusals(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	svc := ranking.NewService(newFakeStore(), func() time.Time { return now })
	object := json.RawMessage(`{"a":1}`)

	cases := map[string]struct {
		call func() error
		want error
	}{
		"reading someone else's model": {func() error { _, err := svc.Get(context.Background(), me, other); return err }, ranking.ErrForbidden},
		"saving someone else's model":  {func() error { _, err := svc.Put(context.Background(), me, other, object, now); return err }, ranking.ErrForbidden},
		"a model that is not an object": {func() error {
			_, err := svc.Put(context.Background(), me, me, json.RawMessage(`[1,2]`), now)
			return err
		}, ranking.ErrInvalid},
		"a model that is not JSON": {func() error {
			_, err := svc.Put(context.Background(), me, me, json.RawMessage(`{"a":`), now)
			return err
		}, ranking.ErrInvalid},
		"an empty model":           {func() error { _, err := svc.Put(context.Background(), me, me, nil, now); return err }, ranking.ErrInvalid},
		"no training time":         {func() error { _, err := svc.Put(context.Background(), me, me, object, time.Time{}); return err }, ranking.ErrInvalid},
		"a user with no model yet": {func() error { _, err := svc.Get(context.Background(), me, me); return err }, ranking.ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStoreFailuresAreWrapped(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("database down")
	svc := ranking.NewService(store, func() time.Time { return now })
	me := uuid.New()

	if _, err := svc.Get(context.Background(), me, me); err == nil || errors.Is(err, ranking.ErrNotFound) {
		t.Fatalf("Get err = %v, want a wrapped store failure", err)
	}
	if _, err := svc.Put(context.Background(), me, me, json.RawMessage(`{}`), now); !errors.Is(err, store.err) {
		t.Fatalf("Put err = %v, want the store failure wrapped", err)
	}
}
