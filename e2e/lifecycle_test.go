//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The e2e lifecycle scenarios through the whole stack: the full flow
//   with credits, cancel and expiry refunds, concurrent accepts, the K limit,
//   self-accept, withdrawal, auto-confirm and admin cancel (D-045 to D-050).
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// maxActive is K, compose.yaml's default (D-049).
const maxActive = 2

// concurrentCouriers is how many couriers race for one errand.
const concurrentCouriers = 10

// TestFullFlowPaysTheCourier walks one errand from create to confirm and
// follows the credits: held on create (D-047), paid to the courier when it
// completes (D-048).
func TestFullFlowPaysTheCourier(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	requester0, courier0 := s.startingBalance(t, requester), s.startingBalance(t, courier)

	const reward = 7
	e := s.createErrand(t, requester, s.errandInput(t, requester, reward)).Errand
	require.Equal(t, requester.id, e.RequesterID)
	// The reward is reserved before the errand is saved (D-047), so the hold
	// shows as soon as create answers.
	s.requireBalance(t, requester, balance{requester0.available - reward, requester0.reserved + reward})

	e = detailOf(t, s.accept(t, courier, e), http.StatusOK).Errand
	require.Equal(t, stateAccepted, e.State)
	require.Equal(t, courier.id, e.CourierID)
	e = detailOf(t, s.act(t, courier, e.ID, "pickup"), http.StatusOK).Errand
	require.Equal(t, statePickedUp, e.State)
	e = detailOf(t, s.act(t, courier, e.ID, "deliver"), http.StatusOK).Errand
	require.Equal(t, stateDelivered, e.State)
	require.NotNil(t, e.ConfirmBy, "a DELIVERED errand shows when it confirms itself (F3.4.2)")
	e = detailOf(t, s.act(t, requester, e.ID, "confirm"), http.StatusOK).Errand
	require.Equal(t, stateCompleted, e.State)
	require.Equal(t, []string{"CREATE", "ACCEPT", "PICK_UP", "DELIVER", "CONFIRM"},
		actionsOf(s.errand(t, requester, e.ID).History))

	// errand.completed moves the hold to the courier (D-048).
	s.waitForBalance(t, requester, balance{requester0.available - reward, requester0.reserved})
	s.waitForBalance(t, courier, balance{courier0.available + reward, courier0.reserved})
}

// TestRequesterCancelRefundsTheReward cancels an OPEN errand and expects the
// hold back (F3.11, D-048).
func TestRequesterCancelRefundsTheReward(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester := s.newStudent(t)
	start := s.startingBalance(t, requester)

	const reward = 5
	e := s.createErrand(t, requester, s.errandInput(t, requester, reward)).Errand
	s.requireBalance(t, requester, balance{start.available - reward, start.reserved + reward})

	e = detailOf(t, s.act(t, requester, e.ID, "cancel"), http.StatusOK).Errand
	require.Equal(t, stateCancelled, e.State)
	require.Equal(t, []string{"CREATE", "CANCEL"}, actionsOf(s.errand(t, requester, e.ID).History))

	// errand.cancelled releases the hold (D-048).
	s.waitForBalance(t, requester, start)
}

// TestExpiredErrandRefundsTheReward lets an errand pass T with no courier
// (D-049). It needs the stack started with T in seconds (README.md).
func TestExpiredErrandRefundsTheReward(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	start := s.startingBalance(t, requester)

	const reward = 3
	created := s.createErrand(t, requester, s.errandInput(t, requester, reward))
	e := created.Errand
	require.NotNil(t, e.ExpiresAt, "an OPEN errand shows its deadline (F3.2.3)")
	requireTimer(t, "T", "ORDER_EXPIRY_PERIOD", "E2E_EXPIRY_PERIOD",
		e.ExpiresAt.Sub(created.ServerTime), s.cfg.expiryPeriod)

	// User actions check the deadline themselves (D-049), so once it has
	// passed no courier can accept, whether or not the sweeper has run yet.
	s.waitForServerTime(t, requester, e.ID, e.ExpiresAt.Add(clockMargin), s.cfg.expiryPeriod+eventDeadline)
	expectStatus(t, s.accept(t, courier, e), http.StatusConflict)

	// The sweeper runs every 5 seconds (D-049).
	d := s.waitForState(t, requester, e.ID, stateExpired, sweepInterval+eventDeadline)
	require.Equal(t, []string{"CREATE", "EXPIRE"}, actionsOf(d.History))
	// errand.expired releases the hold (D-048).
	s.waitForBalance(t, requester, start)
}

// TestConcurrentAcceptsHaveOneWinner has ten couriers accept the same errand
// at once: exactly one wins and the rest are told it is taken (F3.3.3, D-046).
func TestConcurrentAcceptsHaveOneWinner(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester := s.newStudent(t)
	couriers := make([]user, concurrentCouriers)
	for i := range couriers {
		couriers[i] = s.newStudent(t)
	}
	s.startingBalance(t, requester)
	e := s.createErrand(t, requester, s.errandInput(t, requester, 2)).Errand

	// Every request waits on start, so all ten leave together.
	ctx := t.Context()
	start := make(chan struct{})
	results := make([]response, len(couriers))
	errs := make([]error, len(couriers))
	var wg sync.WaitGroup
	for i, c := range couriers {
		wg.Go(func() {
			<-start
			results[i], errs[i] = s.request(ctx, http.MethodPost, actionPath(e.ID, "accept"), c.token, versionRequest{Version: e.Version}, nil)
		})
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, r := range results {
		require.NoError(t, errs[i])
		switch r.status {
		case http.StatusOK:
			require.Equal(t, -1, winner, "a second accept succeeded: %s", r)
			winner = i
		case http.StatusConflict:
		default:
			t.Fatalf("courier %d: %s; want 200 for one courier and 409 for the rest", i, r)
		}
	}
	require.NotEqual(t, -1, winner, "no accept succeeded")

	d := s.errand(t, requester, e.ID)
	require.Equal(t, stateAccepted, d.Errand.State)
	require.Equal(t, couriers[winner].id, d.Errand.CourierID)
	require.Equal(t, []string{"CREATE", "ACCEPT"}, actionsOf(d.History))
}

// TestCourierAtCapacityCannotAccept fills a courier's K slots, expects the
// next accept to be refused, then frees a slot by delivering (F3.3.4, D-045).
func TestCourierAtCapacityCannotAccept(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	in := s.errandInput(t, requester, 1)
	errands := make([]errand, maxActive+1)
	for i := range errands {
		errands[i] = s.createErrand(t, requester, in).Errand
	}
	for _, e := range errands[:maxActive] {
		expectStatus(t, s.accept(t, courier, e), http.StatusOK)
	}
	extra := errands[maxActive]
	if r := s.accept(t, courier, extra); r.status != http.StatusConflict {
		t.Fatalf("%s; want 409 with K = %d. Does the stack run with another ORDER_MAX_ACTIVE_ASSIGNMENTS?", r, maxActive)
	}

	// Only ACCEPTED and PICKED_UP count towards K, so a delivery frees a slot.
	first := errands[0].ID
	expectStatus(t, s.act(t, courier, first, "pickup"), http.StatusOK)
	expectStatus(t, s.act(t, courier, first, "deliver"), http.StatusOK)
	expectStatus(t, s.accept(t, courier, extra), http.StatusOK)
}

// TestRequesterCannotAcceptOwnErrand expects 403 and an errand left untouched
// (F3.3.1, D-045).
func TestRequesterCannotAcceptOwnErrand(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester := s.newStudent(t)
	s.startingBalance(t, requester)

	e := s.createErrand(t, requester, s.errandInput(t, requester, 1)).Errand
	expectStatus(t, s.accept(t, requester, e), http.StatusForbidden)

	d := s.errand(t, requester, e.ID)
	require.Equal(t, stateOpen, d.Errand.State)
	require.Empty(t, d.Errand.CourierID)
}

// TestWithdrawnErrandCanBeAcceptedAgain has a courier give an errand back: it
// is OPEN again with a fresh deadline, another courier can take it, and the
// first keeps a record of the release (F3.12, F3.5.3, D-049).
func TestWithdrawnErrandCanBeAcceptedAgain(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, first, second := s.newStudent(t), s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	created := s.createErrand(t, requester, s.errandInput(t, requester, 2)).Errand
	require.NotNil(t, created.ExpiresAt)
	e := detailOf(t, s.accept(t, first, created), http.StatusOK).Errand
	require.Equal(t, first.id, e.CourierID)

	e = detailOf(t, s.act(t, first, e.ID, "withdraw"), http.StatusOK).Errand
	require.Equal(t, stateOpen, e.State)
	require.Empty(t, e.CourierID)
	// Withdrawing restarts T (D-049). Both deadlines are read back from
	// PostgreSQL at microsecond precision, so one that was kept, or recomputed
	// from created_at, compares equal; only a restart is strictly later.
	require.NotNil(t, e.ExpiresAt)
	require.True(t, e.ExpiresAt.After(*created.ExpiresAt),
		"withdraw did not restart T: deadline %s, first %s", e.ExpiresAt, created.ExpiresAt)

	e = detailOf(t, s.accept(t, second, e), http.StatusOK).Errand
	require.Equal(t, stateAccepted, e.State)
	require.Equal(t, second.id, e.CourierID)
	require.Equal(t, []string{"CREATE", "ACCEPT", "WITHDRAW", "ACCEPT"},
		actionsOf(s.errand(t, requester, e.ID).History))

	released := slices.ContainsFunc(s.assignmentHistory(t, first), func(a assignmentRecord) bool {
		return a.ErrandID == e.ID && a.Outcome == "RELEASED"
	})
	require.True(t, released, "%s's assignment history has no RELEASED record for %s", first.username, e.ID)
}

// TestUnconfirmedDeliveryAutoConfirms delivers an errand the requester never
// confirms: once C passes it completes and the courier is paid (F3.9.4,
// D-049). It needs the stack started with C in seconds (README.md).
func TestUnconfirmedDeliveryAutoConfirms(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	requester0, courier0 := s.startingBalance(t, requester), s.startingBalance(t, courier)

	const reward = 4
	e := s.createErrand(t, requester, s.errandInput(t, requester, reward)).Errand
	expectStatus(t, s.accept(t, courier, e), http.StatusOK)
	expectStatus(t, s.act(t, courier, e.ID, "pickup"), http.StatusOK)
	delivered := detailOf(t, s.act(t, courier, e.ID, "deliver"), http.StatusOK)
	require.NotNil(t, delivered.Errand.ConfirmBy, "a DELIVERED errand shows when it confirms itself (F3.4.2)")
	requireTimer(t, "C", "ORDER_CONFIRMATION_WINDOW", "E2E_CONFIRMATION_WINDOW",
		delivered.Errand.ConfirmBy.Sub(delivered.ServerTime), s.cfg.confirmWindow)

	// The sweeper confirms it once C has passed (D-049).
	d := s.waitForState(t, requester, e.ID, stateCompleted, s.cfg.confirmWindow+sweepInterval+eventDeadline)
	require.Equal(t, []string{"CREATE", "ACCEPT", "PICK_UP", "DELIVER", "AUTO_CONFIRM"}, actionsOf(d.History))
	s.waitForBalance(t, requester, balance{requester0.available - reward, requester0.reserved})
	s.waitForBalance(t, courier, balance{courier0.available + reward, courier0.reserved})
}

// TestAdminCancelsAcceptedErrand has an ADMIN cancel an errand its requester
// can no longer cancel: the requester is refunded and the courier is not paid
// (D-045, D-050).
func TestAdminCancelsAcceptedErrand(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier, admin := s.newStudent(t), s.newStudent(t), s.admin(t)
	requester0, courier0 := s.startingBalance(t, requester), s.startingBalance(t, courier)

	const reward = 6
	e := s.createErrand(t, requester, s.errandInput(t, requester, reward)).Errand
	expectStatus(t, s.accept(t, courier, e), http.StatusOK)
	// The requester's own cancel ends at ACCEPTED (F3.11.1).
	expectStatus(t, s.act(t, requester, e.ID, "cancel"), http.StatusConflict)

	e = detailOf(t, s.act(t, admin, e.ID, "cancel"), http.StatusOK).Errand
	require.Equal(t, stateCancelled, e.State)
	require.Equal(t, []string{"CREATE", "ACCEPT", "ADMIN_CANCEL"}, actionsOf(s.errand(t, requester, e.ID).History))

	s.waitForBalance(t, requester, requester0)
	// The refund above shows errand.cancelled has been applied; the courier
	// was never owed anything.
	s.requireBalance(t, courier, courier0)
}
