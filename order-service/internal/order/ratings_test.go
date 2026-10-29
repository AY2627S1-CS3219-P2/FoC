// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Unit tests for ratings and their gate on post and accept (D-093).
// Author review: PENDING — <reviewer to complete>

package order_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

func TestEachSideRatesTheOther(t *testing.T) {
	f := newFixture(t)
	o := f.seedUnrated()

	byRequester, err := f.svc.Rate(f.ctx, f.requester, o.ID, 4)
	require.NoError(t, err)
	assert.Equal(t, order.Rating{
		OrderID: o.ID, RaterID: f.requester.UserID, RateeID: f.courier.UserID,
		RaterRole: order.RaterRequester, Stars: 4, RatedAt: f.clock.Now(),
	}, byRequester)

	byCourier, err := f.svc.Rate(f.ctx, f.courier, o.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, f.requester.UserID, byCourier.RateeID)
	assert.Equal(t, order.RaterCourier, byCourier.RaterRole)

	assert.Len(t, f.repo.Ratings(), 2)
}

func TestRateRefuses(t *testing.T) {
	f := newFixture(t)
	completed := f.seedUnrated()
	accepted := f.seed(order.StateAccepted)
	cases := []struct {
		name   string
		caller order.Caller
		id     uuid.UUID
		stars  int
		want   order.Kind
	}{
		{"no stars", f.requester, completed.ID, 0, order.KindValidation},
		{"six stars", f.requester, completed.ID, 6, order.KindValidation},
		{"an ADMIN", f.admin, completed.ID, 5, order.KindForbidden},
		{"a stranger, who cannot see a completed errand", f.stranger, completed.ID, 5, order.KindNotFound},
		{"an unknown errand", f.requester, uuid.New(), 5, order.KindNotFound},
		{"an errand not yet completed", f.requester, accepted.ID, 5, order.KindConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.Rate(f.ctx, c.caller, c.id, c.stars)
			requireKind(t, err, c.want)
		})
	}
	assert.Empty(t, f.repo.Ratings(), "nothing refused was saved")
}

func TestARatingCannotBeGivenTwiceOrChanged(t *testing.T) {
	f := newFixture(t)
	o := f.seedUnrated()
	_, err := f.svc.Rate(f.ctx, f.courier, o.ID, 5)
	require.NoError(t, err)

	_, err = f.svc.Rate(f.ctx, f.courier, o.ID, 1)

	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "you have already rated this errand", order.MessageOf(err))
	require.Len(t, f.repo.Ratings(), 1)
	assert.Equal(t, 5, f.repo.Ratings()[0].Stars)
}

func TestAStoreFailureWhileRatingIsNotTheCallersFault(t *testing.T) {
	f := newFixture(t)
	o := f.seedUnrated()
	f.repo.FailNext("SaveRating", ordertest.ErrInjected)

	_, err := f.svc.Rate(f.ctx, f.requester, o.ID, 5)

	require.ErrorIs(t, err, ordertest.ErrInjected)
	assert.Zero(t, order.KindOf(err), "an internal error, not a domain one")
}

func TestAnUnratedErrandBlocksPostingAndAccepting(t *testing.T) {
	f := newFixture(t)
	other := f.seed(order.StateOpen) // posted earlier, by the fixture's requester
	open := f.seed(order.StateOpen)
	open.RequesterID = f.stranger.UserID
	f.repo.Put(open)
	done := f.seedUnrated()

	// The requester cannot post, and the courier cannot accept.
	_, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())
	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "rate your completed errand before posting or accepting another", order.MessageOf(err))
	_, err = f.svc.Accept(f.ctx, f.courier, other.ID, other.Version, "corr")
	requireKind(t, err, order.KindConflict)

	// It blocks each side in both roles: the courier cannot post either.
	courierPost := f.createInput()
	_, _, err = f.svc.Create(f.ctx, f.courier, courierPost)
	requireKind(t, err, order.KindConflict)

	// Rating clears it for that side only.
	_, err = f.svc.Rate(f.ctx, f.courier, done.ID, 5)
	require.NoError(t, err)
	_, err = f.svc.Accept(f.ctx, f.courier, open.ID, open.Version, "corr")
	require.NoError(t, err)
	_, _, err = f.svc.Create(f.ctx, f.requester, f.createInput())
	requireKind(t, err, order.KindConflict)

	_, err = f.svc.Rate(f.ctx, f.requester, done.ID, 5)
	require.NoError(t, err)
	_, _, err = f.svc.Create(f.ctx, f.requester, f.createInput())
	require.NoError(t, err)
}

func TestARetriedCreateThatAlreadyWentThroughIsNotBlocked(t *testing.T) {
	// The gate comes after the duplicate-submission replay (D-047): a retry of
	// a create that succeeded returns its errand, even if a rating became due
	// in between.
	f := newFixture(t)
	in := f.createInput()
	in.RequestKey = "retry-key"
	first, _, err := f.svc.Create(f.ctx, f.requester, in)
	require.NoError(t, err)
	f.seedUnrated()

	again, replayed, err := f.svc.Create(f.ctx, f.requester, in)

	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first.ID, again.ID)
}

func TestPendingRatingsListsWhatIsLeftToRate(t *testing.T) {
	f := newFixture(t)
	older := f.seedUnrated()
	f.clock.Advance(time.Minute)
	newer := f.seedUnrated()
	f.seed(order.StateCompleted) // already rated by both

	got, err := f.svc.PendingRatings(f.ctx, f.requester)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []uuid.UUID{newer.ID, older.ID}, []uuid.UUID{got[0].OrderID, got[1].OrderID})
	assert.Equal(t, f.courier.UserID, got[0].RateeID)
	assert.Equal(t, order.RaterRequester, got[0].RaterRole)
	assert.Equal(t, newer.ItemDescription, got[0].ItemDescription)

	asCourier, err := f.svc.PendingRatings(f.ctx, f.courier)
	require.NoError(t, err)
	require.Len(t, asCourier, 2)
	assert.Equal(t, order.RaterCourier, asCourier[0].RaterRole)
	assert.Equal(t, f.requester.UserID, asCourier[0].RateeID)

	none, err := f.svc.PendingRatings(f.ctx, f.admin)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestUserRatingAveragesWhatTheUserReceived(t *testing.T) {
	f := newFixture(t)
	for _, stars := range []int{5, 4, 2} {
		o := f.seedUnrated()
		_, err := f.svc.Rate(f.ctx, f.requester, o.ID, stars)
		require.NoError(t, err)
	}

	got, err := f.svc.UserRating(f.ctx, f.courier.UserID)
	require.NoError(t, err)
	assert.Equal(t, 3, got.Count)
	assert.InDelta(t, 11.0/3, got.Average, 1e-9)

	unrated, err := f.svc.UserRating(f.ctx, f.stranger.UserID)
	require.NoError(t, err)
	assert.Equal(t, order.UserRating{UserID: f.stranger.UserID}, unrated)
}
