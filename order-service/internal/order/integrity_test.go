// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-15
// Scope: Regression tests for three integration fixes: a settling write
//   re-checks the version it read (D-046, D-048), an admin cannot cancel a
//   DELIVERED errand once C has passed (D-049, F3.9.4), and history hides
//   delivery-note changes from non-parties (F3.6.2, D-053).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

// An amend that commits between a cancel's read and its write must not let the
// cancel go through with the reward it read: the refund event would carry the
// old amount while credit holds the new one.
func TestASettlingWriteRechecksTheVersionItRead(t *testing.T) {
	tests := []struct {
		name   string
		cancel func(f *fixture, o order.Order) error
	}{
		{"requester cancel", func(f *fixture, o order.Order) error {
			_, err := f.svc.Cancel(f.ctx, f.requester, o.ID, "corr-cancel")
			return err
		}},
		{"admin cancel", func(f *fixture, o order.Order) error {
			_, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "corr-cancel")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			o := f.post()
			f.repo.beforeNext("Transition", func() {
				_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
					Version: o.Version, Reward: ptr(8), CorrelationID: "corr-amend",
				})
				require.NoError(t, err)
			})

			err := tc.cancel(f, o)

			requireKind(t, err, order.KindConflict)
			got := f.stored(o.ID)
			assert.Equal(t, order.StateOpen, got.order.State, "the cancel must not have applied")
			assert.Equal(t, 8, got.order.Reward)
			for _, e := range got.events {
				assert.NotEqual(t, order.RoutingCancelled, e.RoutingKey, "no refund with a stale amount")
			}
			hold, _ := f.credit.Hold(o.ID)
			assert.Equal(t, 8, hold)

			sent := f.repo.sentTransitions()
			require.NotEmpty(t, sent)
			require.NotNil(t, sent[0].ExpectVersion, "the write must carry the version it read")
			assert.Equal(t, o.Version, *sent[0].ExpectVersion)
		})
	}
}

// Once C has passed, a DELIVERED errand is completing (F3.9.4), so an admin
// cancel, which would refund the requester and leave the courier unpaid, is
// refused even before the sweeper runs (D-049). Completing it stays allowed.
// The clock sits exactly on C, which already counts as passed.
func TestAdminCannotCancelOnceTheConfirmationWindowHasPassed(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateDelivered) // confirms itself in twelve hours

	assert.Contains(t, order.AllowedActions(o, f.admin, f.clock.Now()), order.APICancel,
		"before C the admin may still cancel")

	f.clock.Advance(12 * time.Hour)
	require.Equal(t, *o.ConfirmBy, f.clock.Now(), "exactly at C, which already counts as passed")
	assert.NotContains(t, order.AllowedActions(o, f.admin, f.clock.Now()), order.APICancel)
	assert.Contains(t, order.AllowedActions(o, f.admin, f.clock.Now()), order.APIComplete)

	_, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "corr-admin")
	requireKind(t, err, order.KindConflict)
	assert.Equal(t, "the confirmation window has passed; this errand is being completed", order.MessageOf(err))

	done, err := f.svc.Complete(f.ctx, f.admin, o.ID, "corr-admin")
	require.NoError(t, err)
	assert.Equal(t, order.StateCompleted, done.State)
}

// A stranger may see an OPEN errand but not its delivery note (D-053), so the
// note must not leak through an AMEND entry's changes either.
func TestHistoryHidesNoteChangesFromNonParties(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
		Version: o.Version, DeliveryNote: ptr("Red cap, level 2"), Reward: ptr(6), CorrelationID: "corr-amend",
	})
	require.NoError(t, err)

	fields := func(c order.Caller) []string {
		_, history, err := f.svc.Get(f.ctx, c, o.ID)
		require.NoError(t, err)
		var out []string
		for _, h := range history {
			if h.Action != order.ActionAmend {
				continue
			}
			for _, ch := range h.Changes {
				out = append(out, ch.Field)
			}
		}
		return out
	}

	assert.ElementsMatch(t, []string{order.FieldDeliveryNote, order.FieldReward}, fields(f.requester))
	assert.ElementsMatch(t, []string{order.FieldDeliveryNote, order.FieldReward}, fields(f.admin))
	assert.ElementsMatch(t, []string{order.FieldReward}, fields(f.stranger))
}

// AI-generated (edited by James Teo).
// The courier may read the note as it is now, but not what it was: a note the
// requester cleared or replaced, perhaps one written for an earlier courier,
// stays with the requester and ADMIN. The courier still sees that it changed.
func TestHistoryShowsTheCourierNoteChangesWithoutTheirValues(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	amended, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
		Version: o.Version, DeliveryNote: ptr(""), Reward: ptr(6), CorrelationID: "corr-amend",
	})
	require.NoError(t, err)
	_, err = f.svc.Accept(f.ctx, f.courier, o.ID, amended.Version, "corr-accept")
	require.NoError(t, err)

	amendOf := func(c order.Caller) []order.FieldChange {
		t.Helper()
		_, history, err := f.svc.Get(f.ctx, c, o.ID)
		require.NoError(t, err)
		for _, h := range history {
			if h.Action == order.ActionAmend {
				return h.Changes
			}
		}
		t.Fatal("no AMEND entry")
		return nil
	}

	full := []order.FieldChange{
		{Field: order.FieldDeliveryNote, From: "Blue shirt, by the lift", To: ""},
		{Field: order.FieldReward, From: "5", To: "6"},
	}
	assert.Equal(t, full, amendOf(f.requester))
	assert.Equal(t, full, amendOf(f.admin))
	assert.Equal(t, []order.FieldChange{
		{Field: order.FieldDeliveryNote, From: "", To: ""},
		{Field: order.FieldReward, From: "5", To: "6"},
	}, amendOf(f.courier))
	_, history, err := f.svc.Get(f.ctx, f.requester, o.ID)
	require.NoError(t, err)
	assert.Equal(t, full, history[1].Changes, "blanking the courier's copy left the stored entry alone")
}
