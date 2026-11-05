//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-31
// Scope: The e2e scenario for D-093's ratings: the gate that holds both sides
//   of a completed errand until each has rated it, the pending list, rating
//   once only, and a user's average, against order-service's recorded spec.
// Author review: nigeltzy — Checked that the boilerplate end-to-end tests were generated and according to the required test and expected behaviour.

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Which side of an errand a rater was (order-service's RaterRole).
const (
	raterRequester = "requester"
	raterCourier   = "courier"
)

// rateRequest is order-service's RateRequest.
type rateRequest struct {
	Stars int `json:"stars"`
}

// rating is order-service's Rating.
type rating struct {
	ErrandID  string `json:"errandId"`
	RaterID   string `json:"raterId"`
	RateeID   string `json:"rateeId"`
	RaterRole string `json:"raterRole"`
	Stars     int    `json:"stars"`
}

// pendingRating is order-service's PendingRating.
type pendingRating struct {
	ErrandID  string `json:"errandId"`
	RateeID   string `json:"rateeId"`
	RaterRole string `json:"raterRole"`
}

type pendingRatingList struct {
	Items []pendingRating `json:"items"`
}

// userRating is order-service's UserRating; Average is absent while Count is 0.
type userRating struct {
	UserID  string   `json:"userId"`
	Count   int      `json:"count"`
	Average *float64 `json:"average"`
}

// rate has u give the other side of errand id stars.
func (s *stack) rate(t *testing.T, u user, id string, stars int) response {
	t.Helper()
	return s.send(t, http.MethodPost, actionPath(id, "rating"), u.token, rateRequest{Stars: stars})
}

// pendingRatings is u's COMPLETED errands they have not rated yet.
func (s *stack) pendingRatings(t *testing.T, u user) []pendingRating {
	t.Helper()
	r := s.send(t, http.MethodGet, ordersPath+"/ratings/pending", u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var list pendingRatingList
	require.NoError(t, r.decode(&list))
	return list.Items
}

// ratingOf reads the ratings userID has received, as viewer.
func (s *stack) ratingOf(t *testing.T, viewer user, userID string) userRating {
	t.Helper()
	r := s.send(t, http.MethodGet, ordersPath+"/users/"+userID+"/rating", viewer.token, nil)
	expectStatus(t, r, http.StatusOK)
	var got userRating
	require.NoError(t, r.decode(&got))
	return got
}

// tryCreateErrand is createErrand for a create that may be refused.
func (s *stack) tryCreateErrand(t *testing.T, u user, in createErrandRequest) response {
	t.Helper()
	header := http.Header{}
	header.Set("X-Request-Id", randomUUID())
	r, err := s.request(t.Context(), http.MethodPost, ordersPath, u.token, in, header)
	require.NoError(t, err)
	return r
}

// complete takes e from OPEN to COMPLETED: courier accepts, picks up and
// delivers it, and requester confirms.
func (s *stack) complete(t *testing.T, requester, courier user, e errand) errand {
	t.Helper()
	expectStatus(t, s.accept(t, courier, e), http.StatusOK)
	expectStatus(t, s.act(t, courier, e.ID, "pickup"), http.StatusOK)
	expectStatus(t, s.act(t, courier, e.ID, "deliver"), http.StatusOK)
	done := detailOf(t, s.act(t, requester, e.ID, "confirm"), http.StatusOK).Errand
	require.Equal(t, stateCompleted, done.State)
	return done
}

// TestRatingGateHoldsBothSidesUntilTheyRate completes an errand, then expects
// its requester to be refused a new errand and its courier another accept
// until each has rated the other once; the ratings then show in each one's
// average (D-093).
func TestRatingGateHoldsBothSidesUntilTheyRate(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier, stranger := s.newStudent(t), s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	in := s.errandInput(t, requester, 1)
	done := s.createErrand(t, requester, in).Errand
	// Posted now, while the requester may still post, for the courier to try
	// to accept once the gate is shut.
	next := s.createErrand(t, requester, in).Errand

	// Only a COMPLETED errand can be rated.
	expectStatus(t, s.rate(t, requester, done.ID, 5), http.StatusConflict)
	done = s.complete(t, requester, courier, done)

	require.Equal(t, []pendingRating{{ErrandID: done.ID, RateeID: courier.id, RaterRole: raterRequester}},
		s.pendingRatings(t, requester))
	require.Equal(t, []pendingRating{{ErrandID: done.ID, RateeID: requester.id, RaterRole: raterCourier}},
		s.pendingRatings(t, courier))

	// The gate is shut for both sides.
	expectStatus(t, s.tryCreateErrand(t, requester, in), http.StatusConflict)
	expectStatus(t, s.accept(t, courier, next), http.StatusConflict)

	// Out of range stars, and someone who was on neither side, are refused
	// and leave the errand unrated. A COMPLETED errand is hidden from someone
	// on neither side, so it is not found; one they can see, an OPEN errand,
	// is forbidden.
	expectStatus(t, s.rate(t, courier, done.ID, 6), http.StatusBadRequest)
	expectStatus(t, s.rate(t, stranger, done.ID, 3), http.StatusNotFound)
	expectStatus(t, s.rate(t, stranger, next.ID, 3), http.StatusForbidden)

	// The courier rates the requester, once, and may accept again.
	r := s.rate(t, courier, done.ID, 2)
	expectStatus(t, r, http.StatusCreated)
	var got rating
	require.NoError(t, r.decode(&got))
	require.Equal(t, rating{ErrandID: done.ID, RaterID: courier.id, RateeID: requester.id, RaterRole: raterCourier, Stars: 2}, got)
	expectStatus(t, s.rate(t, courier, done.ID, 5), http.StatusConflict)
	require.Empty(t, s.pendingRatings(t, courier))
	expectStatus(t, s.accept(t, courier, next), http.StatusOK)

	// The requester's own gate stays shut until they rate too.
	expectStatus(t, s.tryCreateErrand(t, requester, in), http.StatusConflict)
	r = s.rate(t, requester, done.ID, 4)
	expectStatus(t, r, http.StatusCreated)
	require.NoError(t, r.decode(&got))
	require.Equal(t, rating{ErrandID: done.ID, RaterID: requester.id, RateeID: courier.id, RaterRole: raterRequester, Stars: 4}, got)
	expectStatus(t, s.rate(t, requester, done.ID, 1), http.StatusConflict)
	require.Empty(t, s.pendingRatings(t, requester))
	expectStatus(t, s.tryCreateErrand(t, requester, in), http.StatusCreated)

	// Each side's rating is readable by any signed-in user; the refused
	// second ratings did not count.
	four, two := 4.0, 2.0
	require.Equal(t, userRating{UserID: courier.id, Count: 1, Average: &four}, s.ratingOf(t, stranger, courier.id))
	require.Equal(t, userRating{UserID: requester.id, Count: 1, Average: &two}, s.ratingOf(t, stranger, requester.id))
	require.Equal(t, userRating{UserID: stranger.id, Count: 0}, s.ratingOf(t, courier, stranger.id))
}
