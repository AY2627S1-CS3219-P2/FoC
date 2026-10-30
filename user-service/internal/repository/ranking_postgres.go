// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The PostgreSQL adapter for ranking.Store over the separate ranking
//   database (D-095).
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"errors"
	"fmt"

	"foc/user-service/internal/ranking"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	getRankingModelQuery = `
		SELECT user_id, model, trained_at, updated_at
		FROM ranking_models
		WHERE user_id = $1`
	putRankingModelQuery = `
		INSERT INTO ranking_models (user_id, model, trained_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET model = EXCLUDED.model, trained_at = EXCLUDED.trained_at, updated_at = EXCLUDED.updated_at`
)

// RankingModelRepository implements ranking.Store over its own pool, which is
// a connection to the ranking database, not the users database.
type RankingModelRepository struct {
	pool *pgxpool.Pool
}

// NewRankingModelRepository returns a ready-to-use repository over pool.
func NewRankingModelRepository(pool *pgxpool.Pool) *RankingModelRepository {
	return &RankingModelRepository{pool: pool}
}

var _ ranking.Store = (*RankingModelRepository)(nil)

// Get implements ranking.Store.
func (r *RankingModelRepository) Get(ctx context.Context, userID uuid.UUID) (ranking.Model, error) {
	var m ranking.Model
	var data []byte
	err := r.pool.QueryRow(ctx, getRankingModelQuery, userID).Scan(&m.UserID, &data, &m.TrainedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ranking.Model{}, ranking.ErrNotFound
	}
	if err != nil {
		return ranking.Model{}, fmt.Errorf("select ranking model: %w", err)
	}
	m.Data = data
	m.TrainedAt = m.TrainedAt.UTC()
	m.UpdatedAt = m.UpdatedAt.UTC()
	return m, nil
}

// Put implements ranking.Store.
func (r *RankingModelRepository) Put(ctx context.Context, m ranking.Model) error {
	if _, err := r.pool.Exec(ctx, putRankingModelQuery, m.UserID, []byte(m.Data), m.TrainedAt, m.UpdatedAt); err != nil {
		return fmt.Errorf("upsert ranking model: %w", err)
	}
	return nil
}
