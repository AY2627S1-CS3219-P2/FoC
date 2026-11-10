//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: notification-service through the gateway (backlog F5, D-140 to D-146):
//   who is told what along an errand's life, and a user's own list. Every test
//   is named TestNotification*, so a run with the service stopped can leave them
//   out with `-skip TestNotification`; preflight does not need the service,
//   because nothing else does (D-140).
// Author review: nigeltzy - Checked that the end-to-end test implementation seems correct and as expected according to requirements.

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const notificationsPath = "/api/notifications"

type notificationRecord struct {
	ID          string    `json:"id"`
	ErrandID    string    `json:"errandId"`
	StateChange string    `json:"stateChange"`
	Message     string    `json:"message"`
	Read        bool      `json:"read"`
	CreatedAt   time.Time `json:"createdAt"`
}

type notificationPage struct {
	Notifications []notificationRecord `json:"notifications"`
	Total         int                  `json:"total"`
}

func (s *stack) notificationsOf(ctx context.Context, u user) (notificationPage, error) {
	r, err := s.request(ctx, http.MethodGet, notificationsPath+"?limit=100", u.token, nil, nil)
	if err != nil {
		return notificationPage{}, err
	}
	if r.status != http.StatusOK {
		return notificationPage{}, fmt.Errorf("list notifications: %s", r)
	}
	var page notificationPage
	return page, r.decode(&page)
}

// waitForNotification polls until u has a notification of stateChange about the
// errand, and returns it.
func (s *stack) waitForNotification(t *testing.T, u user, errandID, stateChange string) notificationRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), eventDeadline)
	defer cancel()
	var found notificationRecord
	status, ok := poll(ctx, func(ctx context.Context) (bool, string) {
		page, err := s.notificationsOf(ctx, u)
		if err != nil {
			return false, err.Error()
		}
		for _, n := range page.Notifications {
			if n.ErrandID == errandID && n.StateChange == stateChange {
				found = n
				return true, ""
			}
		}
		return false, fmt.Sprintf("%d notifications, none is %s", len(page.Notifications), stateChange)
	})
	require.True(t, ok, "no %s notification within %s: %s", stateChange, eventDeadline, status)
	return found
}

// TestNotificationsFollowAnErrandToCompletion walks an errand from accept to
// confirm (F5.2.1 to F5.2.3, F5.3.2).
func TestNotificationsFollowAnErrandToCompletion(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	const reward = 4
	e := s.createErrand(t, requester, s.errandInput(t, requester, reward)).Errand
	e = detailOf(t, s.accept(t, courier, e), http.StatusOK).Errand
	s.waitForNotification(t, requester, e.ID, "errand.accepted")

	detailOf(t, s.act(t, courier, e.ID, "pickup"), http.StatusOK)
	s.waitForNotification(t, requester, e.ID, "errand.picked_up")

	detailOf(t, s.act(t, courier, e.ID, "deliver"), http.StatusOK)
	delivered := s.waitForNotification(t, requester, e.ID, "errand.delivered")
	assert.Contains(t, delivered.Message, "completes by itself after", "F5.2.3 states C")

	detailOf(t, s.act(t, requester, e.ID, "confirm"), http.StatusOK)
	s.waitForNotification(t, requester, e.ID, "errand.completed")
	paid := s.waitForNotification(t, courier, e.ID, "errand.completed")
	assert.Contains(t, paid.Message, fmt.Sprintf("%d credits", reward), "F5.3.2")

	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()
	page, err := s.notificationsOf(ctx, requester)
	require.NoError(t, err)
	var order []string
	for _, n := range page.Notifications {
		order = append(order, n.StateChange)
		assert.False(t, n.Read)
	}
	assert.Equal(t, []string{"errand.completed", "errand.delivered", "errand.picked_up", "errand.accepted"}, order,
		"one each, newest first (F5.1.3, F5.5.2)")
}

// TestNotificationOfAWithdrawalReachesTheRequester covers F5.2.5.
func TestNotificationOfAWithdrawalReachesTheRequester(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	e := s.createErrand(t, requester, s.errandInput(t, requester, 2)).Errand
	e = detailOf(t, s.accept(t, courier, e), http.StatusOK).Errand
	detailOf(t, s.act(t, courier, e.ID, "withdraw"), http.StatusOK)
	s.waitForNotification(t, requester, e.ID, "errand.withdrawn")
}

// TestNotificationOfAnExpiryReachesTheRequester covers F5.2.4. It needs the
// stack started with T in seconds (README.md).
func TestNotificationOfAnExpiryReachesTheRequester(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester := s.newStudent(t)
	s.startingBalance(t, requester)

	e := s.createErrand(t, requester, s.errandInput(t, requester, 2)).Errand
	s.waitForState(t, requester, e.ID, stateExpired, s.cfg.expiryPeriod+sweepInterval+eventDeadline)
	s.waitForNotification(t, requester, e.ID, "errand.expired")
}

// TestNotificationOfAnAdminCancelReachesBothParties covers F5.4: the courier is
// told too, though errand.cancelled does not name them (D-146).
func TestNotificationOfAnAdminCancelReachesBothParties(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier, admin := s.newStudent(t), s.newStudent(t), s.admin(t)
	s.startingBalance(t, requester)

	e := s.createErrand(t, requester, s.errandInput(t, requester, 2)).Errand
	e = detailOf(t, s.accept(t, courier, e), http.StatusOK).Errand
	// Accepted first, so the service has seen the courier before the cancel.
	s.waitForNotification(t, requester, e.ID, "errand.accepted")
	detailOf(t, s.act(t, admin, e.ID, "cancel"), http.StatusOK)

	s.waitForNotification(t, requester, e.ID, "errand.cancelled")
	got := s.waitForNotification(t, courier, e.ID, "errand.cancelled")
	assert.False(t, strings.Contains(strings.ToLower(got.Message), "admin"), "neutral wording (D-141)")
}

// TestNotificationsAreTheirOwnersAlone covers F5.1.2, F5.1.4 and F5.1.5, and the
// paging D-144 records.
func TestNotificationsAreTheirOwnersAlone(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier, stranger := s.newStudent(t), s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	e := s.createErrand(t, requester, s.errandInput(t, requester, 2)).Errand
	e = detailOf(t, s.accept(t, courier, e), http.StatusOK).Errand
	n := s.waitForNotification(t, requester, e.ID, "errand.accepted")
	readPath := notificationsPath + "/" + n.ID + "/read"

	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()
	page, err := s.notificationsOf(ctx, stranger)
	require.NoError(t, err)
	assert.Empty(t, page.Notifications)
	expectStatus(t, s.send(t, http.MethodPost, readPath, stranger.token, nil), http.StatusNotFound)

	expectStatus(t, s.send(t, http.MethodPost, readPath, requester.token, nil), http.StatusNoContent)
	expectStatus(t, s.send(t, http.MethodPost, readPath, requester.token, nil), http.StatusNoContent)
	page, err = s.notificationsOf(ctx, requester)
	require.NoError(t, err)
	require.NotEmpty(t, page.Notifications)
	assert.True(t, page.Notifications[0].Read)

	expectStatus(t, s.send(t, http.MethodGet, notificationsPath, "", nil), http.StatusUnauthorized)
	expectStatus(t, s.send(t, http.MethodGet, notificationsPath+"?limit=0", requester.token, nil), http.StatusBadRequest)
	var one notificationPage
	r := s.send(t, http.MethodGet, notificationsPath+"?limit=1&offset=0", requester.token, nil)
	expectStatus(t, r, http.StatusOK)
	require.NoError(t, r.decode(&one))
	assert.Len(t, one.Notifications, 1)
}
