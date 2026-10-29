//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Real-PostgreSQL tests of the ranking-model repository and its own
//   migrations (D-095).
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"foc/user-service/internal/ranking"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRankingModelRepository(t *testing.T) {
	ctx := context.Background()
	databaseURL := startPostgres(t, ctx)
	migrationsPath, err := filepath.Abs("../../migrations_ranking")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	if err := Apply(databaseURL, (&url.URL{Scheme: "file", Path: migrationsPath}).String()); err != nil {
		t.Fatalf("apply ranking migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := NewRankingModelRepository(pool)
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	t.Run("a user with no model is not found", func(t *testing.T) {
		if _, err := repo.Get(ctx, uuid.New()); !errors.Is(err, ranking.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("a second save replaces the first", func(t *testing.T) {
		id := uuid.New()
		first := ranking.Model{UserID: id, Data: json.RawMessage(`{"v":1}`), TrainedAt: at, UpdatedAt: at}
		second := ranking.Model{UserID: id, Data: json.RawMessage(`{"v":2,"trees":[[{"f":-1,"p":0.5}]]}`), TrainedAt: at.Add(time.Hour), UpdatedAt: at.Add(time.Hour)}
		if err := repo.Put(ctx, first); err != nil {
			t.Fatalf("Put first: %v", err)
		}
		if err := repo.Put(ctx, second); err != nil {
			t.Fatalf("Put second: %v", err)
		}
		got, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		var gotData, wantData any
		if err := json.Unmarshal(got.Data, &gotData); err != nil {
			t.Fatalf("decode stored model: %v", err)
		}
		_ = json.Unmarshal(second.Data, &wantData)
		if !equalJSON(gotData, wantData) || !got.TrainedAt.Equal(second.TrainedAt) || !got.UpdatedAt.Equal(second.UpdatedAt) {
			t.Fatalf("got %+v, want %+v", got, second)
		}
	})

	t.Run("the table refuses a model that is not an object", func(t *testing.T) {
		err := repo.Put(ctx, ranking.Model{UserID: uuid.New(), Data: json.RawMessage(`[1,2]`), TrainedAt: at, UpdatedAt: at})
		if err == nil {
			t.Fatal("Put of an array succeeded")
		}
	})
}

// equalJSON compares decoded JSON: JSONB keeps the values, not the spacing or
// key order.
func equalJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
