// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Unit tests of who is told what (D-141, F5.2 to F5.5) and of the user's
//   own list (F5.1), against an in-memory repository.
// Author review: PENDING — <reviewer to complete>

package notification_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/notification-service/internal/notification"
)

// fakeRepo is an in-memory notification.Repository with the same dedupe and
// ordering rules as the Postgres one.
type fakeRepo struct {
	rows     []notification.Notification
	couriers map[uuid.UUID]notification.CourierChange
	applyErr error
	readErr  error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{couriers: map[uuid.UUID]notification.CourierChange{}}
}

func (f *fakeRepo) Apply(_ context.Context, change *notification.CourierChange, ns []notification.Notification) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	if change != nil {
		if cur, ok := f.couriers[change.ErrandID]; !ok || !change.ChangedAt.Before(cur.ChangedAt) {
			f.couriers[change.ErrandID] = *change
		}
	}
	for _, n := range ns {
		if !slices.ContainsFunc(f.rows, func(r notification.Notification) bool {
			return r.EventID == n.EventID && r.RecipientID == n.RecipientID
		}) {
			f.rows = append(f.rows, n)
		}
	}
	return nil
}

func (f *fakeRepo) CurrentCourier(_ context.Context, errandID uuid.UUID) (*uuid.UUID, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.couriers[errandID].CourierID, nil
}

func (f *fakeRepo) List(_ context.Context, recipient uuid.UUID, limit, offset int) (notification.Page, error) {
	var own []notification.Notification
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].RecipientID == recipient {
			own = append(own, f.rows[i])
		}
	}
	page := notification.Page{Total: len(own)}
	if offset < len(own) {
		page.Notifications = own[offset:min(offset+limit, len(own))]
	}
	return page, nil
}

func (f *fakeRepo) MarkRead(_ context.Context, recipient, id uuid.UUID) error {
	for i := range f.rows {
		if f.rows[i].ID == id && f.rows[i].RecipientID == recipient {
			f.rows[i].Read = true
			return nil
		}
	}
	return notification.ErrNotFound
}

func (f *fakeRepo) to(recipient uuid.UUID) []notification.Notification {
	var out []notification.Notification
	for _, r := range f.rows {
		if r.RecipientID == recipient {
			out = append(out, r)
		}
	}
	return out
}

var (
	requester = uuid.New()
	courier   = uuid.New()
	errand    = uuid.New()
	at        = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
)

func event(kind string, courierID *uuid.UUID) notification.Event {
	return notification.Event{
		ID: uuid.New(), Type: kind, ErrandID: errand, RequesterID: requester,
		CourierID: courierID, Amount: 5, OccurredAt: at,
	}
}

func setup() (*notification.Service, *fakeRepo) {
	repo := newFakeRepo()
	return notification.NewService(repo, 24*time.Hour), repo
}

func TestEachEventTellsTheRightPeople(t *testing.T) {
	for _, tc := range []struct {
		name          string
		event         notification.Event
		requesterText string
		courierText   string
	}{
		{"accepted (F5.2.1)", event(notification.EventAccepted, &courier), "A courier accepted your errand.", ""},
		{"picked up (F5.2.2)", event(notification.EventPickedUp, &courier), "Your errand has been picked up.", ""},
		{"delivered states C (F5.2.3)", event(notification.EventDelivered, &courier),
			"Your errand has been delivered. Please confirm it; it completes by itself after 24 hours.", ""},
		{"expired (F5.2.4)", event(notification.EventExpired, nil), "Your errand expired without being accepted.", ""},
		{"withdrawn (F5.2.5)", event(notification.EventWithdrawn, &courier), "Your courier withdrew, so your errand is open again.", ""},
		{"completed (F5.3.2)", event(notification.EventCompleted, &courier), "Your errand is complete.",
			"Errand complete: 5 credits are being transferred to you."},
		{"cancelled with no courier", event(notification.EventCancelled, nil), "Your errand was cancelled.", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := setup()
			require.NoError(t, svc.Handle(t.Context(), tc.event))

			got := repo.to(requester)
			require.Len(t, got, 1)
			assert.Equal(t, tc.requesterText, got[0].Message)
			assert.Equal(t, tc.event.Type, got[0].StateChange)
			assert.Equal(t, errand, got[0].ErrandID)
			assert.Equal(t, tc.event.ID, got[0].EventID)
			assert.False(t, got[0].Read)

			if tc.courierText == "" {
				assert.Empty(t, repo.to(courier))
				return
			}
			gotCourier := repo.to(courier)
			require.Len(t, gotCourier, 1)
			assert.Equal(t, tc.courierText, gotCourier[0].Message)
		})
	}
}

func TestOneCreditIsSingular(t *testing.T) {
	svc, repo := setup()
	e := event(notification.EventCompleted, &courier)
	e.Amount = 1
	require.NoError(t, svc.Handle(t.Context(), e))
	assert.Equal(t, "Errand complete: 1 credit is being transferred to you.", repo.to(courier)[0].Message)
}

func TestDeliveredStatesTheConfiguredWindow(t *testing.T) {
	for _, tc := range []struct {
		window time.Duration
		want   string
	}{
		{time.Hour, "1 hour"},
		{48 * time.Hour, "48 hours"},
		{90 * time.Minute, "90 minutes"},
		{20 * time.Second, "20 seconds"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			repo := newFakeRepo()
			svc := notification.NewService(repo, tc.window)
			require.NoError(t, svc.Handle(t.Context(), event(notification.EventDelivered, &courier)))
			assert.Contains(t, repo.to(requester)[0].Message, "after "+tc.want+".")
		})
	}
}

func TestACancelTellsTheCourierOnTheErrand(t *testing.T) {
	svc, repo := setup()
	require.NoError(t, svc.Handle(t.Context(), event(notification.EventAccepted, &courier)))
	require.NoError(t, svc.Handle(t.Context(), event(notification.EventCancelled, nil)))

	got := repo.to(courier)
	require.Len(t, got, 1, "F5.4.1: the assigned courier hears about the cancel")
	assert.Equal(t, "An errand assigned to you was cancelled.", got[0].Message)
}

func TestACancelAfterAWithdrawalLeavesTheFormerCourierOut(t *testing.T) {
	svc, repo := setup()
	accepted := event(notification.EventAccepted, &courier)
	withdrawn := event(notification.EventWithdrawn, &courier)
	withdrawn.OccurredAt = at.Add(time.Minute)
	require.NoError(t, svc.Handle(t.Context(), accepted))
	require.NoError(t, svc.Handle(t.Context(), withdrawn))
	require.NoError(t, svc.Handle(t.Context(), event(notification.EventCancelled, nil)))
	assert.Empty(t, repo.to(courier))
}

func TestARedeliveredEventNotifiesOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event notification.Event
	}{
		{"single recipient", event(notification.EventAccepted, &courier)},
		{"two recipients", event(notification.EventCompleted, &courier)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := setup()
			require.NoError(t, svc.Handle(t.Context(), tc.event))
			before := len(repo.rows)
			require.NoError(t, svc.Handle(t.Context(), tc.event), "F6.1.3: a duplicate is still a success")
			assert.Len(t, repo.rows, before, "F5.5.2")
		})
	}
}

func TestEventsWithNoRuleAreIgnored(t *testing.T) {
	svc, repo := setup()
	for _, kind := range []string{"errand.failed", "errand.reward_set", "errand.rated"} {
		require.NoError(t, svc.Handle(t.Context(), event(kind, nil)))
	}
	assert.Empty(t, repo.rows)
}

func TestUnusableEventsAreRefusedForGood(t *testing.T) {
	zero := uuid.Nil
	for _, tc := range []struct {
		name   string
		mutate func(*notification.Event)
	}{
		{"no event id", func(e *notification.Event) { e.ID = uuid.Nil }},
		{"no errand", func(e *notification.Event) { e.ErrandID = uuid.Nil }},
		{"no requester", func(e *notification.Event) { e.RequesterID = uuid.Nil }},
		{"no time", func(e *notification.Event) { e.OccurredAt = time.Time{} }},
		{"accepted without a courier", func(e *notification.Event) { e.CourierID = nil }},
		{"accepted with a zero courier", func(e *notification.Event) { e.CourierID = &zero }},
		{"completed without a courier", func(e *notification.Event) {
			e.Type = notification.EventCompleted
			e.CourierID = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := setup()
			e := event(notification.EventAccepted, &courier)
			tc.mutate(&e)
			require.ErrorIs(t, svc.Handle(t.Context(), e), notification.ErrInvalidEvent)
			assert.Empty(t, repo.rows)
		})
	}
}

func TestStorageFailuresCanBeRetried(t *testing.T) {
	boom := errors.New("database down")
	t.Run("storing", func(t *testing.T) {
		svc, repo := setup()
		repo.applyErr = boom
		err := svc.Handle(t.Context(), event(notification.EventExpired, nil))
		require.ErrorIs(t, err, boom)
		require.NotErrorIs(t, err, notification.ErrInvalidEvent)
	})
	t.Run("finding the courier", func(t *testing.T) {
		svc, repo := setup()
		repo.readErr = boom
		err := svc.Handle(t.Context(), event(notification.EventCancelled, nil))
		require.ErrorIs(t, err, boom)
		assert.Empty(t, repo.rows, "nothing is stored for half an event")
	})
}

func TestListShowsOnlyTheCallersOwnNewestFirst(t *testing.T) {
	svc, _ := setup()
	first := event(notification.EventAccepted, &courier)
	second := event(notification.EventPickedUp, &courier)
	require.NoError(t, svc.Handle(t.Context(), first))
	require.NoError(t, svc.Handle(t.Context(), second))

	page, err := svc.List(t.Context(), requester, notification.DefaultLimit, 0)
	require.NoError(t, err)
	require.Len(t, page.Notifications, 2)
	assert.Equal(t, 2, page.Total)
	assert.Equal(t, second.ID, page.Notifications[0].EventID)
	assert.Equal(t, first.ID, page.Notifications[1].EventID)

	page, err = svc.List(t.Context(), uuid.New(), notification.DefaultLimit, 0)
	require.NoError(t, err)
	assert.Empty(t, page.Notifications, "F5.1.2")
}

func TestListRefusesAnOutOfRangePage(t *testing.T) {
	svc, _ := setup()
	for _, tc := range []struct {
		name          string
		limit, offset int
	}{
		{"zero limit", 0, 0},
		{"limit over the maximum", notification.MaxLimit + 1, 0},
		{"negative offset", 10, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.List(t.Context(), requester, tc.limit, tc.offset)
			require.ErrorIs(t, err, notification.ErrInvalidPage)
		})
	}
}

func TestMarkReadOnlyTouchesTheCallersOwn(t *testing.T) {
	svc, repo := setup()
	require.NoError(t, svc.Handle(t.Context(), event(notification.EventExpired, nil)))
	id := repo.rows[0].ID

	require.ErrorIs(t, svc.MarkRead(t.Context(), uuid.New(), id), notification.ErrNotFound, "F5.1.2")
	assert.False(t, repo.rows[0].Read)

	require.NoError(t, svc.MarkRead(t.Context(), requester, id))
	assert.True(t, repo.rows[0].Read)
	require.NoError(t, svc.MarkRead(t.Context(), requester, id), "already read is not an error")
}
