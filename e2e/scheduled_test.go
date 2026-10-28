//go:build e2e

// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-10-28
// Scope: Whole-stack scheduled-errand tests through the gateway, transcribing
//   order-service/AGENTS.md's Scheduled Errands rules and api/openapi.yaml.
// Author review: PENDING

package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scheduledDetail reads the new wire field without changing the older helpers.
type scheduledDetail struct {
	Errand struct {
		errand
		Scheduled string `json:"scheduled"`
	} `json:"errand"`
	History    []historyEntry `json:"history"`
	ServerTime time.Time      `json:"serverTime"`
}

func scheduledOf(t *testing.T, r response, status int) scheduledDetail {
	t.Helper()
	expectStatus(t, r, status)
	var d scheduledDetail
	require.NoError(t, r.decode(&d))
	return d
}

func (s *stack) scheduleErrand(t *testing.T, u user, reward int, delay time.Duration) scheduledDetail {
	t.Helper()
	// Use the service clock rather than the machine's clock for the schedule.
	r := s.send(t, http.MethodGet, ordersPath+"/requested?scope=active", u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var list struct {
		ServerTime time.Time `json:"serverTime"`
	}
	require.NoError(t, r.decode(&list))
	// PostgreSQL timestamps retain microseconds; use that precision on input.
	at := list.ServerTime.Add(delay).UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	in := struct {
		createErrandRequest
		Scheduled string `json:"scheduled"`
	}{s.errandInput(t, u, reward), at}
	header := http.Header{"X-Request-Id": []string{randomUUID()}}
	r, err := s.request(t.Context(), http.MethodPost, ordersPath, u.token, in, header)
	require.NoError(t, err)
	d := scheduledOf(t, r, http.StatusCreated)
	require.Equal(t, "SCHEDULED", d.Errand.State)
	require.Equal(t, at, d.Errand.Scheduled)
	require.Nil(t, d.Errand.ExpiresAt)
	// The same submission must not reserve a second reward.
	r, err = s.request(t.Context(), http.MethodPost, ordersPath, u.token, in, header)
	require.NoError(t, err)
	replay := scheduledOf(t, r, http.StatusOK)
	require.Equal(t, d.Errand.ID, replay.Errand.ID)
	return d
}

func (s *stack) requireScheduledListed(t *testing.T, u user, path, id string, want bool) {
	t.Helper()
	r := s.send(t, http.MethodGet, path, u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var list struct {
		Items []errand `json:"items"`
	}
	require.NoError(t, r.decode(&list))
	found := false
	for _, e := range list.Items {
		if e.ID == id {
			found = true
		}
	}
	require.Equal(t, want, found, "errand %s in %s", id, path)
}

func TestScheduledFullFlowPaysCourier(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	r0, c0 := s.startingBalance(t, requester), s.startingBalance(t, courier)
	d := s.scheduleErrand(t, requester, 7, 10*time.Second)
	e := d.Errand.errand
	s.requireBalance(t, requester, balance{r0.available - 7, r0.reserved + 7})
	s.requireScheduledListed(t, requester, ordersPath+"/requested?scope=active", e.ID, true)
	s.requireScheduledListed(t, courier, ordersPath+"/open", e.ID, false)
	expectStatus(t, s.send(t, http.MethodGet, errandPath(e.ID), courier.token, nil), http.StatusNotFound)
	expectStatus(t, s.accept(t, courier, e), http.StatusConflict)
	opened := s.waitForState(t, requester, e.ID, stateOpen, 10*time.Second+2*sweepInterval)
	require.NotNil(t, opened.Errand.ExpiresAt)
	assert.Equal(t, []string{"CREATE", "OPEN"}, actionsOf(opened.History))
	assert.Equal(t, s.cfg.expiryPeriod, opened.Errand.ExpiresAt.Sub(opened.Errand.StateChangedAt))
	s.requireBalance(t, requester, balance{r0.available - 7, r0.reserved + 7})
	s.requireScheduledListed(t, courier, ordersPath+"/open", e.ID, true)
	e = detailOf(t, s.accept(t, courier, opened.Errand), http.StatusOK).Errand
	e = detailOf(t, s.act(t, courier, e.ID, "pickup"), http.StatusOK).Errand
	e = detailOf(t, s.act(t, courier, e.ID, "deliver"), http.StatusOK).Errand
	e = detailOf(t, s.act(t, requester, e.ID, "confirm"), http.StatusOK).Errand
	require.Equal(t, stateCompleted, e.State)
	s.waitForBalance(t, requester, balance{r0.available - 7, r0.reserved})
	s.waitForBalance(t, courier, balance{c0.available + 7, c0.reserved})
}

func TestScheduledExpiryRefundsReward(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u := s.newStudent(t)
	start := s.startingBalance(t, u)
	d := s.scheduleErrand(t, u, 5, 8*time.Second)
	opened := s.waitForState(t, u, d.Errand.ID, stateOpen, 8*time.Second+2*sweepInterval)
	require.NotNil(t, opened.Errand.ExpiresAt)
	assert.Equal(t, s.cfg.expiryPeriod, opened.Errand.ExpiresAt.Sub(opened.Errand.StateChangedAt))
	expired := s.waitForState(t, u, d.Errand.ID, stateExpired, s.cfg.expiryPeriod+2*sweepInterval)
	assert.Equal(t, []string{"CREATE", "OPEN", "EXPIRE"}, actionsOf(expired.History))
	s.waitForBalance(t, u, start)
}

func TestScheduledRequesterCancelRefundsReward(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u := s.newStudent(t)
	start := s.startingBalance(t, u)
	d := s.scheduleErrand(t, u, 5, time.Hour)
	cancelled := detailOf(t, s.act(t, u, d.Errand.ID, "cancel"), http.StatusOK)
	require.Equal(t, stateCancelled, cancelled.Errand.State)
	s.waitForBalance(t, u, start)
}

func TestScheduledRescheduleAndOpenImmediately(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u := s.newStudent(t)
	start := s.startingBalance(t, u)
	d := s.scheduleErrand(t, u, 5, time.Hour)
	// An ordinary amendment must preserve SCHEDULED and its timestamp too.
	edited := scheduledOf(t, s.send(t, http.MethodPatch, errandPath(d.Errand.ID), u.token,
		map[string]any{"version": d.Errand.Version, "itemDescription": "Updated coffee order"}), http.StatusOK)
	require.Equal(t, "SCHEDULED", edited.Errand.State)
	require.Equal(t, d.Errand.Scheduled, edited.Errand.Scheduled)
	at := d.ServerTime.Add(2 * time.Hour).UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	amended := scheduledOf(t, s.send(t, http.MethodPatch, errandPath(d.Errand.ID), u.token,
		map[string]any{"version": edited.Errand.Version, "scheduled": at, "reward": 8}), http.StatusOK)
	require.Equal(t, "SCHEDULED", amended.Errand.State)
	require.Equal(t, at, amended.Errand.Scheduled)
	s.requireBalance(t, u, balance{start.available - 8, start.reserved + 8})
	opened := scheduledOf(t, s.send(t, http.MethodPatch, errandPath(d.Errand.ID), u.token,
		map[string]any{"version": amended.Errand.Version, "scheduled": ""}), http.StatusOK)
	require.Equal(t, stateOpen, opened.Errand.State)
	require.Empty(t, opened.Errand.Scheduled)
	require.NotNil(t, opened.Errand.ExpiresAt)
	assert.Equal(t, s.cfg.expiryPeriod, opened.Errand.ExpiresAt.Sub(opened.Errand.StateChangedAt))
	expectStatus(t, s.act(t, u, d.Errand.ID, "cancel"), http.StatusOK)
	s.waitForBalance(t, u, start)
}

func TestScheduledOpenLimitCancellationRefundsReward(t *testing.T) {
	for _, mode := range []string{"automatic", "remove schedule"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := newStack(t)
			u := s.newStudent(t)
			start := s.startingBalance(t, u)
			// Create scheduled first, then fill five OPEN places before it is due.
			delay := 10 * time.Second
			if mode == "remove schedule" {
				delay = time.Hour
			}
			d := s.scheduleErrand(t, u, 5, delay)
			for i := 0; i < 5; i++ {
				s.createErrand(t, u, s.errandInput(t, u, 1))
			}
			// Five OPEN errands must not prevent another scheduled creation.
			extra := s.scheduleErrand(t, u, 1, time.Hour)
			expectStatus(t, s.act(t, u, extra.Errand.ID, "cancel"), http.StatusOK)
			if mode == "remove schedule" {
				cleared := detailOf(t, s.send(t, http.MethodPatch, errandPath(d.Errand.ID), u.token,
					map[string]any{"version": d.Errand.Version, "scheduled": ""}), http.StatusOK)
				require.Equal(t, stateCancelled, cleared.Errand.State)
			} else {
				s.waitForState(t, u, d.Errand.ID, stateCancelled, delay+2*sweepInterval)
			}
			s.waitForBalance(t, u, balance{start.available - 5, start.reserved + 5})
		})
	}
}

func TestScheduledAdminCancellationRefundsReward(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u, admin := s.newStudent(t), s.admin(t)
	start := s.startingBalance(t, u)
	d := s.scheduleErrand(t, u, 5, time.Hour)
	s.requireScheduledListed(t, admin, ordersPath+"?scope=active", d.Errand.ID, true)
	cancelled := detailOf(t, s.act(t, admin, d.Errand.ID, "cancel"), http.StatusOK)
	require.Equal(t, stateCancelled, cancelled.Errand.State)
	s.waitForBalance(t, u, start)
}

func TestScheduledInvalidTimesDoNotReserveCredits(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	u := s.newStudent(t)
	start := s.startingBalance(t, u)
	base := s.errandInput(t, u, 5)
	for _, at := range []string{"not-a-timestamp", "2000-01-01T00:00:00Z", time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339)} {
		r := s.send(t, http.MethodPost, ordersPath, u.token, struct {
			createErrandRequest
			Scheduled string `json:"scheduled"`
		}{base, at})
		expectStatus(t, r, http.StatusBadRequest)
		s.requireBalance(t, u, start)
	}
}
