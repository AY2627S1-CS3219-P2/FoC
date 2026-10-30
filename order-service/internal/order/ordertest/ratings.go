// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The in-memory RatingStore and ActivityStore (D-092, D-093), kept on
//   the fake Repository because pending ratings are read from its errands.
// Author review: PENDING — <reviewer to complete>

package ordertest

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
)

var (
	_ order.RatingStore   = (*Repository)(nil)
	_ order.ActivityStore = (*Repository)(nil)
)

// ShownRow is one errand RecordShown stored.
type ShownRow struct {
	CourierID uuid.UUID
	Shown     order.Shown
	Filters   order.Filters
	At        time.Time
}

// SearchRow is one search RecordSearch stored.
type SearchRow struct {
	CourierID uuid.UUID
	Filters   order.Filters
	At        time.Time
}

// SaveRating stores r unless its rater already rated that errand.
func (r *Repository) SaveRating(_ context.Context, rating order.Rating) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("SaveRating"); err != nil {
		return err
	}
	if r.ratedBy(rating.OrderID, rating.RaterID) {
		return order.ErrAlreadyRated
	}
	r.ratings = append(r.ratings, rating)
	return nil
}

func (r *Repository) ratedBy(orderID, raterID uuid.UUID) bool {
	return slices.ContainsFunc(r.ratings, func(x order.Rating) bool {
		return x.OrderID == orderID && x.RaterID == raterID
	})
}

// PendingRatings returns userID's COMPLETED errands they have not rated, most
// recently completed first.
func (r *Repository) PendingRatings(_ context.Context, userID uuid.UUID, limit int) ([]order.PendingRating, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("PendingRatings"); err != nil {
		return nil, err
	}
	return limited(r.pending(userID), limit), nil
}

// HasPendingRating reports whether userID has any COMPLETED errand to rate.
func (r *Repository) HasPendingRating(_ context.Context, userID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("HasPendingRating"); err != nil {
		return false, err
	}
	return len(r.pending(userID)) > 0, nil
}

func (r *Repository) pending(userID uuid.UUID) []order.PendingRating {
	var out []order.PendingRating
	for _, o := range r.orders {
		if o.State != order.StateCompleted || o.CourierID == nil || r.ratedBy(o.ID, userID) {
			continue
		}
		p := order.PendingRating{OrderID: o.ID, ItemDescription: o.ItemDescription, CompletedAt: o.StateChangedAt}
		switch userID {
		case o.RequesterID:
			p.RaterRole, p.RateeID = order.RaterRequester, *o.CourierID
		case *o.CourierID:
			p.RaterRole, p.RateeID = order.RaterCourier, o.RequesterID
		default:
			continue
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b order.PendingRating) int { return b.CompletedAt.Compare(a.CompletedAt) })
	return out
}

// UserRating returns the count and mean of the ratings userID received.
func (r *Repository) UserRating(_ context.Context, userID uuid.UUID) (order.UserRating, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("UserRating"); err != nil {
		return order.UserRating{}, err
	}
	out := order.UserRating{UserID: userID}
	sum := 0
	for _, x := range r.ratings {
		if x.RateeID == userID {
			out.Count++
			sum += x.Stars
		}
	}
	if out.Count > 0 {
		out.Average = float64(sum) / float64(out.Count)
	}
	return out, nil
}

// Ratings returns every rating stored, oldest first.
func (r *Repository) Ratings() []order.Rating {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.ratings)
}

// RecordShown stores one row per item naming a known errand.
func (r *Repository) RecordShown(_ context.Context, courierID uuid.UUID, items []order.Shown, f order.Filters, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("RecordShown"); err != nil {
		return err
	}
	for _, it := range items {
		if _, ok := r.orders[it.OrderID]; ok {
			r.shown = append(r.shown, ShownRow{CourierID: courierID, Shown: it, Filters: f, At: at})
		}
	}
	return nil
}

// RecordSearch stores the search.
func (r *Repository) RecordSearch(_ context.Context, courierID uuid.UUID, f order.Filters, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("RecordSearch"); err != nil {
		return err
	}
	r.searches = append(r.searches, SearchRow{CourierID: courierID, Filters: f, At: at})
	return nil
}

// ShownRows returns every stored shown row, oldest first.
func (r *Repository) ShownRows() []ShownRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.shown)
}

// SearchRows returns every stored search, oldest first.
func (r *Repository) SearchRows() []SearchRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.searches)
}
