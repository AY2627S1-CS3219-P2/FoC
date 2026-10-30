// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: PostgreSQL adapter for order.RatingStore and order.ActivityStore
//   (D-092, D-093) over the tables of migration 000004.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
)

var (
	_ order.RatingStore   = (*Postgres)(nil)
	_ order.ActivityStore = (*Postgres)(nil)
)

// ratingsPrimaryKey is the constraint that refuses a second rating.
const ratingsPrimaryKey = "errand_ratings_pkey"

// pendingRatingsQuery selects $1's COMPLETED errands, as requester or courier,
// with no rating by $1: the errand, whom to rate, which side $1 was, the item,
// and when it completed.
const pendingRatingsQuery = `
	SELECT o.id,
	       CASE WHEN o.requester_id = $1 THEN o.courier_id ELSE o.requester_id END,
	       CASE WHEN o.requester_id = $1 THEN 'requester' ELSE 'courier' END,
	       o.item_description,
	       o.state_changed_at
	FROM orders o
	WHERE o.state = 'COMPLETED'
	  AND (o.requester_id = $1 OR o.courier_id = $1)
	  AND o.courier_id IS NOT NULL
	  AND NOT EXISTS (
	      SELECT 1 FROM errand_ratings r WHERE r.order_id = o.id AND r.rater_id = $1)`

// SaveRating implements order.RatingStore.
func (p *Postgres) SaveRating(ctx context.Context, r order.Rating) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO errand_ratings (order_id, rater_id, ratee_id, rater_role, stars, rated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		r.OrderID, r.RaterID, r.RateeID, string(r.RaterRole), r.Stars, r.RatedAt)
	switch {
	case isUniqueViolation(err, ratingsPrimaryKey):
		return order.ErrAlreadyRated
	case err != nil:
		return fmt.Errorf("insert rating: %w", err)
	}
	return nil
}

// PendingRatings implements order.RatingStore.
func (p *Postgres) PendingRatings(ctx context.Context, userID uuid.UUID, limit int) ([]order.PendingRating, error) {
	rows, err := p.pool.Query(ctx, pendingRatingsQuery+` ORDER BY o.state_changed_at DESC, o.id LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending ratings: %w", err)
	}
	defer rows.Close()
	var out []order.PendingRating
	for rows.Next() {
		var pr order.PendingRating
		var role string
		if err := rows.Scan(&pr.OrderID, &pr.RateeID, &role, &pr.ItemDescription, &pr.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan pending rating: %w", err)
		}
		pr.RaterRole = order.RaterRole(role)
		pr.CompletedAt = pr.CompletedAt.UTC()
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pending ratings: %w", err)
	}
	return out, nil
}

// HasPendingRating implements order.RatingStore.
func (p *Postgres) HasPendingRating(ctx context.Context, userID uuid.UUID) (bool, error) {
	var pending bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (`+pendingRatingsQuery+`)`, userID).Scan(&pending); err != nil {
		return false, fmt.Errorf("check pending ratings: %w", err)
	}
	return pending, nil
}

// UserRating implements order.RatingStore.
func (p *Postgres) UserRating(ctx context.Context, userID uuid.UUID) (order.UserRating, error) {
	out := order.UserRating{UserID: userID}
	var avg *float64
	err := p.pool.QueryRow(ctx,
		`SELECT count(*), avg(stars)::float8 FROM errand_ratings WHERE ratee_id = $1`, userID,
	).Scan(&out.Count, &avg)
	if err != nil {
		return order.UserRating{}, fmt.Errorf("read user rating: %w", err)
	}
	if avg != nil {
		out.Average = *avg
	}
	return out, nil
}

// filtersJSON is how Filters are kept in the filters columns.
type filtersJSON struct {
	TimeLeft string `json:"timeLeft"`
	From     string `json:"from"`
	To       string `json:"to"`
}

func marshalFilters(f order.Filters) ([]byte, error) {
	b, err := json.Marshal(filtersJSON(f))
	if err != nil {
		return nil, fmt.Errorf("encode filters: %w", err)
	}
	return b, nil
}

// RecordShown implements order.ActivityStore. One statement inserts every
// item, and the join on orders drops ids order-service does not know.
func (p *Postgres) RecordShown(ctx context.Context, courierID uuid.UUID, items []order.Shown, f order.Filters, at time.Time) error {
	filters, err := marshalFilters(f)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, len(items))
	positions := make([]int32, len(items))
	for i, it := range items {
		ids[i] = it.OrderID
		positions[i] = int32(it.Position)
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO errand_views (courier_id, order_id, position, filters, shown_at)
		SELECT $1, o.id, s.position, $4, $5
		FROM unnest($2::uuid[], $3::int[]) AS s (order_id, position)
		JOIN orders o ON o.id = s.order_id`,
		courierID, ids, positions, filters, at)
	if err != nil {
		return fmt.Errorf("insert shown errands: %w", err)
	}
	return nil
}

// RecordSearch implements order.ActivityStore.
func (p *Postgres) RecordSearch(ctx context.Context, courierID uuid.UUID, f order.Filters, at time.Time) error {
	filters, err := marshalFilters(f)
	if err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO errand_searches (courier_id, filters, searched_at) VALUES ($1, $2, $3)`,
		courierID, filters, at); err != nil {
		return fmt.Errorf("insert search: %w", err)
	}
	return nil
}
