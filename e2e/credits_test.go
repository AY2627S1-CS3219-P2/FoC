//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The e2e check that the browser cannot call credit-service's write
//   endpoints through the gateway, and that trying moves no credits.
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Bodies credit-service accepts on its reserve and adjust routes (its
// CreateReservationRequest and AdjustRewardRequest).
type reservationRequest struct {
	ErrandID string `json:"errandId"`
	Amount   int    `json:"amount"`
}

type adjustRewardRequest struct {
	NewReward   int `json:"newReward"`
	CurrVersion int `json:"currVersion"`
}

// TestBrowserCannotWriteCredits sends the two credit writes meant only for
// order-service (D-047, D-056, D-057) through the gateway with a student's token.
// Neither may reach credit-service: forwarded, each would move this student's
// credits.
func TestBrowserCannotWriteCredits(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester := s.newStudent(t)
	start := s.startingBalance(t, requester)

	const reward = 3
	e := s.createErrand(t, requester, s.errandInput(t, requester, reward)).Errand
	held := balance{start.available - reward, start.reserved + reward}
	s.requireBalance(t, requester, held)

	writes := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"reserve", http.MethodPost, creditsPath + "/reservations",
			reservationRequest{ErrandID: randomUUID(), Amount: 1}},
		{"adjust", http.MethodPatch, creditsPath + "/reservations/" + e.ID,
			adjustRewardRequest{NewReward: reward + 10, CurrVersion: e.Version}},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			r := s.send(t, w.method, w.path, requester.token, w.body)
			expectStatus(t, r, http.StatusNotFound, http.StatusMethodNotAllowed)
		})
	}

	// Nothing moved: the hold is still exactly the errand's reward.
	s.requireBalance(t, requester, held)
	require.Equal(t, reward, s.errand(t, requester, e.ID).Errand.Reward)
}

// AI-generated (edited by James Teo).
// TestRewardChangeMovesTheHold raises an errand's reward, lowers it, then
// cancels it, and follows the requester's credits after each step: the hold
// tracks the reward (D-102), and the cancel returns all of it, whatever the
// hold was, because errand.cancelled settles by its amount and credit-service
// returns any hold left over (D-059).
func TestRewardChangeMovesTheHold(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester := s.newStudent(t)
	start := s.startingBalance(t, requester)
	holding := func(reward int) balance {
		return balance{start.available - reward, start.reserved + reward}
	}

	const posted, raised, lowered = 3, 8, 2
	e := s.createErrand(t, requester, s.errandInput(t, requester, posted)).Errand
	s.requireBalance(t, requester, holding(posted))

	e = s.amendReward(t, requester, e, raised)
	require.Equal(t, raised, e.Reward)
	s.waitForBalance(t, requester, holding(raised))

	e = s.amendReward(t, requester, e, lowered)
	require.Equal(t, lowered, e.Reward)
	s.waitForBalance(t, requester, holding(lowered))

	e = detailOf(t, s.act(t, requester, e.ID, "cancel"), http.StatusOK).Errand
	require.Equal(t, stateCancelled, e.State)
	s.waitForBalance(t, requester, start)
}
