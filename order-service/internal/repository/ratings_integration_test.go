//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Integration tests of the PostgreSQL RatingStore and ActivityStore
//   (D-092, D-093), run from TestPostgres.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func (e *env) testRatings(t *testing.T) {
	ctx := t.Context()
	completed := func(requester, courier uuid.UUID, minutes int) order.Order {
		o := openErrand(requester, at(0))
		return heldBy(o, courier, order.StateCompleted, at(time.Duration(minutes)*time.Minute))
	}
	rating := func(o order.Order, rater uuid.UUID, stars int) order.Rating {
		r := order.Rating{OrderID: o.ID, RaterID: rater, Stars: stars, RatedAt: at(time.Hour)}
		if rater == o.RequesterID {
			r.RateeID, r.RaterRole = *o.CourierID, order.RaterRequester
		} else {
			r.RateeID, r.RaterRole = o.RequesterID, order.RaterCourier
		}
		return r
	}

	t.Run("a second rating by the same rater is refused", func(t *testing.T) {
		e.reset(t)
		o := completed(uuid.New(), uuid.New(), 1)
		e.insert(t, o)

		require.NoError(t, e.repo.SaveRating(ctx, rating(o, o.RequesterID, 4)))
		require.ErrorIs(t, e.repo.SaveRating(ctx, rating(o, o.RequesterID, 1)), order.ErrAlreadyRated)
		require.NoError(t, e.repo.SaveRating(ctx, rating(o, *o.CourierID, 2)), "the other side may still rate")
		assert.Equal(t, 2, e.count(t, `SELECT count(*) FROM errand_ratings`))
	})

	t.Run("the table refuses what the domain refuses", func(t *testing.T) {
		e.reset(t)
		o := completed(uuid.New(), uuid.New(), 1)
		e.insert(t, o)
		self := rating(o, o.RequesterID, 5)
		self.RateeID = self.RaterID
		badRole := rating(o, o.RequesterID, 5)
		badRole.RaterRole = "admin"
		noErrand := rating(o, o.RequesterID, 5)
		noErrand.OrderID = uuid.New()
		for _, bad := range []order.Rating{
			rating(o, o.RequesterID, 0), rating(o, o.RequesterID, 6), self, badRole, noErrand,
		} {
			err := e.repo.SaveRating(ctx, bad)
			require.Error(t, err)
			assert.NotErrorIs(t, err, order.ErrAlreadyRated)
		}
	})

	t.Run("pending ratings are each side's unrated completed errands, newest first", func(t *testing.T) {
		e.reset(t)
		me, courier, other := uuid.New(), uuid.New(), uuid.New()
		olderMine := completed(me, courier, 1)
		newerMine := completed(me, courier, 2)
		carriedByMe := completed(other, me, 3)
		ratedByMe := completed(me, courier, 4)
		notMine := completed(other, courier, 5)
		stillOpen := openErrand(me, at(0))
		cancelled := heldBy(openErrand(me, at(0)), courier, order.StateCancelled, at(6*time.Minute))
		e.insert(t, olderMine, newerMine, carriedByMe, ratedByMe, notMine, stillOpen, cancelled)
		require.NoError(t, e.repo.SaveRating(ctx, rating(ratedByMe, me, 5)))
		// The courier rating newerMine does not count for me.
		require.NoError(t, e.repo.SaveRating(ctx, rating(newerMine, courier, 5)))

		got, err := e.repo.PendingRatings(ctx, me, 10)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, []uuid.UUID{carriedByMe.ID, newerMine.ID, olderMine.ID},
			[]uuid.UUID{got[0].OrderID, got[1].OrderID, got[2].OrderID})
		assert.Equal(t, order.PendingRating{
			OrderID: carriedByMe.ID, RateeID: other, RaterRole: order.RaterCourier,
			ItemDescription: latte, CompletedAt: at(3 * time.Minute),
		}, got[0])
		assert.Equal(t, order.RaterRequester, got[1].RaterRole)
		assert.Equal(t, courier, got[1].RateeID)

		limited, err := e.repo.PendingRatings(ctx, me, 1)
		require.NoError(t, err)
		assert.Len(t, limited, 1)

		pending, err := e.repo.HasPendingRating(ctx, me)
		require.NoError(t, err)
		assert.True(t, pending)
		pending, err = e.repo.HasPendingRating(ctx, uuid.New())
		require.NoError(t, err)
		assert.False(t, pending)
	})

	t.Run("a user's rating averages what they received", func(t *testing.T) {
		e.reset(t)
		courier := uuid.New()
		var total int
		for i, stars := range []int{5, 4, 2} {
			o := completed(uuid.New(), courier, i)
			e.insert(t, o)
			require.NoError(t, e.repo.SaveRating(ctx, rating(o, o.RequesterID, stars)))
			total += stars
		}

		got, err := e.repo.UserRating(ctx, courier)
		require.NoError(t, err)
		assert.Equal(t, 3, got.Count)
		assert.InDelta(t, float64(total)/3, got.Average, 1e-9)

		nobody, err := e.repo.UserRating(ctx, uuid.New())
		require.NoError(t, err)
		assert.Zero(t, nobody.Count)
		assert.Zero(t, nobody.Average)
	})
}

func (e *env) testActivity(t *testing.T) {
	ctx := t.Context()
	filters := order.Filters{TimeLeft: "under30m", From: uuid.NewString(), To: "place:COM3 Lobby"}

	t.Run("shown errands are stored one row each, unknown ids skipped", func(t *testing.T) {
		e.reset(t)
		courier := uuid.New()
		a, b := openErrand(uuid.New(), at(0)), openErrand(uuid.New(), at(0))
		e.insert(t, a, b)

		require.NoError(t, e.repo.RecordShown(ctx, courier, []order.Shown{
			{OrderID: a.ID, Position: 0}, {OrderID: uuid.New(), Position: 1}, {OrderID: b.ID, Position: 2},
		}, filters, at(time.Minute)))

		rows, err := e.pool.Query(ctx, `SELECT courier_id, order_id, position, filters, shown_at FROM errand_views ORDER BY position`)
		require.NoError(t, err)
		defer rows.Close()
		type row struct {
			courier, order uuid.UUID
			position       int
			filters        filtersJSON
		}
		var got []row
		for rows.Next() {
			var r row
			var raw []byte
			var shownAt time.Time
			require.NoError(t, rows.Scan(&r.courier, &r.order, &r.position, &raw, &shownAt))
			require.NoError(t, json.Unmarshal(raw, &r.filters))
			assert.True(t, shownAt.Equal(at(time.Minute)))
			got = append(got, r)
		}
		require.NoError(t, rows.Err())
		assert.Equal(t, []row{
			{courier, a.ID, 0, filtersJSON(filters)},
			{courier, b.ID, 2, filtersJSON(filters)},
		}, got)
	})

	t.Run("a search is stored with its filters", func(t *testing.T) {
		e.reset(t)
		courier := uuid.New()

		require.NoError(t, e.repo.RecordSearch(ctx, courier, filters, at(time.Minute)))

		var raw []byte
		require.NoError(t, e.pool.QueryRow(ctx,
			`SELECT filters FROM errand_searches WHERE courier_id = $1`, courier).Scan(&raw))
		var got filtersJSON
		require.NoError(t, json.Unmarshal(raw, &got))
		assert.Equal(t, filtersJSON(filters), got)
	})
}
