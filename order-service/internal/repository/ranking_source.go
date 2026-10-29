// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The PostgreSQL ranking.Source: the errands a courier was shown and
//   whether they took them, their history, and requesters' ratings (D-088).
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"foc/order-service/internal/ranking"
	"foc/order-service/internal/shuttle"
)

var _ ranking.Source = (*Postgres)(nil)

// maxSearchesRead caps how many of a courier's searches the features read,
// newest first.
const maxSearchesRead = 500

const (
	// shownExamplesSQL takes each errand shown to $1 at its first showing,
	// with whether $1 accepted it at or after that moment (D-088's label),
	// newest showing first.
	shownExamplesSQL = `
		SELECT order_id, shown_at, requester_id, reward, created_at, supplier_id, supplier_name,
			supplier_type, supplier_lat, supplier_lon, delivery_location, accepted
		FROM (
			SELECT DISTINCT ON (v.order_id)
				v.order_id, v.shown_at, o.requester_id, o.reward, o.created_at, o.supplier_id,
				o.supplier_name, o.supplier_type, o.supplier_lat, o.supplier_lon, o.delivery_location,
				EXISTS (SELECT 1 FROM order_history h
					WHERE h.order_id = v.order_id AND h.action = 'ACCEPT'
						AND h.courier_id = $1 AND h.occurred_at >= v.shown_at) AS accepted
			FROM errand_views v
			JOIN orders o ON o.id = v.order_id
			WHERE v.courier_id = $1 AND o.requester_id <> $1
			ORDER BY v.order_id, v.shown_at, v.id
		) first_shown
		ORDER BY shown_at DESC, order_id
		LIMIT $2`

	// courierAcceptsSQL lists $1's accepts with when they stopped carrying
	// each: the first delivery, withdrawal or cancellation after the accept.
	courierAcceptsSQL = `
		SELECT h.order_id, h.occurred_at, o.supplier_type, o.supplier_lat, o.supplier_lon,
			o.delivery_location, e.occurred_at
		FROM order_history h
		JOIN orders o ON o.id = h.order_id
		LEFT JOIN LATERAL (
			SELECT x.occurred_at FROM order_history x
			WHERE x.order_id = h.order_id AND x.id > h.id
				AND x.action IN ('DELIVER', 'WITHDRAW', 'CANCEL', 'ADMIN_CANCEL', 'ADMIN_COMPLETE')
			ORDER BY x.id
			LIMIT 1
		) e ON true
		WHERE h.action = 'ACCEPT' AND h.courier_id = $1
		ORDER BY h.occurred_at`

	courierCompletionsSQL = `
		SELECT state_changed_at FROM orders WHERE courier_id = $1 AND state = 'COMPLETED'`

	ratingsReceivedSQL = `
		SELECT r.ratee_id, r.stars, r.rated_at, o.supplier_type
		FROM errand_ratings r
		JOIN orders o ON o.id = r.order_id
		WHERE r.ratee_id = ANY($1)`

	courierSearchesSQL = `
		SELECT filters, searched_at FROM errand_searches
		WHERE courier_id = $1
		ORDER BY searched_at DESC
		LIMIT $2`
)

// ShownExamples implements ranking.Source.
func (p *Postgres) ShownExamples(ctx context.Context, courierID uuid.UUID, limit int) ([]ranking.Example, error) {
	rows, err := p.pool.Query(ctx, shownExamplesSQL, courierID, limit)
	if err != nil {
		return nil, fmt.Errorf("query shown examples: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ranking.Example, error) {
		var e ranking.Example
		var lat, lon *float64
		err := row.Scan(&e.OrderID, &e.ShownAt, &e.RequesterID, &e.Reward, &e.CreatedAt, &e.SupplierID,
			&e.SupplierName, &e.SupplierType, &lat, &lon, &e.DeliveryLocation, &e.Accepted)
		e.Pickup = point(lat, lon)
		e.ShownAt, e.CreatedAt = e.ShownAt.UTC(), e.CreatedAt.UTC()
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("read shown examples: %w", err)
	}
	return out, nil
}

// CourierHistory implements ranking.Source. RequesterRatings is left empty.
func (p *Postgres) CourierHistory(ctx context.Context, courierID uuid.UUID) (ranking.History, error) {
	var h ranking.History

	rows, err := p.pool.Query(ctx, courierAcceptsSQL, courierID)
	if err != nil {
		return ranking.History{}, fmt.Errorf("query accepts: %w", err)
	}
	h.Accepts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ranking.Accept, error) {
		var a ranking.Accept
		var lat, lon *float64
		err := row.Scan(&a.OrderID, &a.At, &a.SupplierType, &lat, &lon, &a.DeliveryLocation, &a.Until)
		a.Pickup = point(lat, lon)
		a.At, a.Until = a.At.UTC(), utcPtr(a.Until)
		return a, err
	})
	if err != nil {
		return ranking.History{}, fmt.Errorf("read accepts: %w", err)
	}

	rows, err = p.pool.Query(ctx, courierCompletionsSQL, courierID)
	if err != nil {
		return ranking.History{}, fmt.Errorf("query completions: %w", err)
	}
	h.Completions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (time.Time, error) {
		var t time.Time
		err := row.Scan(&t)
		return t.UTC(), err
	})
	if err != nil {
		return ranking.History{}, fmt.Errorf("read completions: %w", err)
	}

	received, err := p.RatingsReceived(ctx, []uuid.UUID{courierID})
	if err != nil {
		return ranking.History{}, err
	}
	h.Ratings = received[courierID]

	rows, err = p.pool.Query(ctx, courierSearchesSQL, courierID, maxSearchesRead)
	if err != nil {
		return ranking.History{}, fmt.Errorf("query searches: %w", err)
	}
	h.Searches, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ranking.Search, error) {
		var raw []byte
		var s ranking.Search
		if err := row.Scan(&raw, &s.At); err != nil {
			return s, err
		}
		var f filtersJSON
		if err := json.Unmarshal(raw, &f); err != nil {
			return s, fmt.Errorf("decode filters: %w", err)
		}
		s.From, s.To, s.At = f.From, f.To, s.At.UTC()
		return s, nil
	})
	if err != nil {
		return ranking.History{}, fmt.Errorf("read searches: %w", err)
	}
	return h, nil
}

// RatingsReceived implements ranking.Source.
func (p *Postgres) RatingsReceived(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]ranking.Rating, error) {
	out := map[uuid.UUID][]ranking.Rating{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, ratingsReceivedSQL, userIDs)
	if err != nil {
		return nil, fmt.Errorf("query ratings received: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var r ranking.Rating
		if err := rows.Scan(&id, &r.Stars, &r.At, &r.SupplierType); err != nil {
			return nil, fmt.Errorf("scan rating: %w", err)
		}
		r.At = r.At.UTC()
		out[id] = append(out[id], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read ratings received: %w", err)
	}
	return out, nil
}

// point is a supplier position from its two columns, or nil when there is none.
func point(lat, lon *float64) *shuttle.Point {
	if lat == nil || lon == nil {
		return nil
	}
	return &shuttle.Point{Lat: *lat, Lon: *lon}
}
