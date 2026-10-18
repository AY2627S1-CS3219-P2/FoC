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
	NewReward int `json:"newReward"`
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
			adjustRewardRequest{NewReward: reward + 10}},
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
