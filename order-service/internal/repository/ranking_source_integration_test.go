//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Integration tests of the PostgreSQL ranking.Source (D-088), run
//   from TestPostgres.
// Author review: PENDING — <reviewer to complete>

package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func (e *env) testRankingSource(t *testing.T) {
	ctx := t.Context()
	courier := uuid.New()
	history := func(o order.Order, action order.Action, to order.State, at time.Time) {
		e.insertHistory(t, order.HistoryEntry{
			OrderID: o.ID, Action: action, ToState: to, ActorID: &courier,
			ActorKind: order.ActorCourier, CourierID: &courier, OccurredAt: at, CorrelationID: "test",
		})
	}
	shown := func(o order.Order, at time.Time) {
		require.NoError(t, e.repo.RecordShown(ctx, courier, []order.Shown{{OrderID: o.ID}}, order.Filters{TimeLeft: "any"}, at))
	}

	t.Run("shown errands, once each, labelled by a later accept", func(t *testing.T) {
		e.reset(t)
		taken := heldBy(openErrand(uuid.New(), at(0)), courier, order.StateAccepted, at(3*time.Minute))
		passed := openErrand(uuid.New(), at(0))
		takenBefore := heldBy(openErrand(uuid.New(), at(0)), courier, order.StateAccepted, at(time.Minute))
		own := openErrand(courier, at(0))
		e.insert(t, taken, passed, takenBefore, own)
		shown(taken, at(2*time.Minute))
		shown(taken, at(4*time.Minute)) // a second showing: not a second example
		shown(passed, at(5*time.Minute))
		shown(takenBefore, at(2*time.Minute)) // shown after it was accepted
		shown(own, at(6*time.Minute))
		history(taken, order.ActionAccept, order.StateAccepted, at(3*time.Minute))
		history(takenBefore, order.ActionAccept, order.StateAccepted, at(time.Minute))

		got, err := e.repo.ShownExamples(ctx, courier, 10)
		require.NoError(t, err)
		require.Len(t, got, 3, "the courier's own errand is left out")
		assert.Equal(t, passed.ID, got[0].OrderID, "newest first")
		assert.False(t, got[0].Accepted)
		byID := map[uuid.UUID]bool{}
		for _, ex := range got {
			byID[ex.OrderID] = ex.Accepted
		}
		assert.True(t, byID[taken.ID])
		assert.False(t, byID[takenBefore.ID], "an accept before the showing is no answer to it")
		for _, ex := range got {
			if ex.OrderID == taken.ID {
				assert.Equal(t, at(2*time.Minute), ex.ShownAt, "its first showing")
			}
		}

		limited, err := e.repo.ShownExamples(ctx, courier, 1)
		require.NoError(t, err)
		assert.Len(t, limited, 1)
	})

	t.Run("history: accepts with when carrying ended, completions, ratings and searches", func(t *testing.T) {
		e.reset(t)
		delivered := heldBy(openErrand(uuid.New(), at(0)), courier, order.StateCompleted, at(20*time.Minute))
		delivered.Supplier.Type = "Food"
		carrying := heldBy(openErrand(uuid.New(), at(0)), courier, order.StateAccepted, at(30*time.Minute))
		e.insert(t, delivered, carrying)
		_, err := e.pool.Exec(ctx, `UPDATE orders SET supplier_type = 'Food' WHERE id = $1`, delivered.ID)
		require.NoError(t, err)
		history(delivered, order.ActionAccept, order.StateAccepted, at(time.Minute))
		history(delivered, order.ActionDeliver, order.StateDelivered, at(10*time.Minute))
		history(carrying, order.ActionAccept, order.StateAccepted, at(30*time.Minute))
		require.NoError(t, e.repo.SaveRating(ctx, order.Rating{
			OrderID: delivered.ID, RaterID: delivered.RequesterID, RateeID: courier,
			RaterRole: order.RaterRequester, Stars: 4, RatedAt: at(25 * time.Minute),
		}))
		require.NoError(t, e.repo.RecordSearch(ctx, courier, order.Filters{TimeLeft: "any", To: "area:UTown"}, at(5*time.Minute)))

		h, err := e.repo.CourierHistory(ctx, courier)
		require.NoError(t, err)
		require.Len(t, h.Accepts, 2)
		assert.Equal(t, delivered.ID, h.Accepts[0].OrderID)
		assert.Equal(t, "Food", h.Accepts[0].SupplierType)
		require.NotNil(t, h.Accepts[0].Until)
		assert.Equal(t, at(10*time.Minute), *h.Accepts[0].Until, "carried until delivered")
		assert.Nil(t, h.Accepts[1].Until, "still carried")
		assert.Equal(t, []time.Time{at(20 * time.Minute)}, h.Completions)
		require.Len(t, h.Ratings, 1)
		assert.Equal(t, 4, h.Ratings[0].Stars)
		assert.Equal(t, "Food", h.Ratings[0].SupplierType)
		require.Len(t, h.Searches, 1)
		assert.Equal(t, "area:UTown", h.Searches[0].To)
	})
}
