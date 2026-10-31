// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-10-28
// Scope: Regression tests of scheduled errands against order-service/AGENTS.md
//   and the existing admin cancellation rule (D-045).
// Author review: PENDING

package order_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func TestScheduledAuditAdminCanCancelNonFinalErrand(t *testing.T) {
	f := newFixture(t)
	o := f.seed(order.StateScheduled)
	assert.Contains(t, order.AllowedActions(o, f.admin, f.clock.Now()), order.APICancel)
	got, err := f.svc.Cancel(f.ctx, f.admin, o.ID, "scheduled-admin-cancel")
	require.NoError(t, err, "D-045 permits admin cancellation of a non-final errand")
	assert.Equal(t, order.StateCancelled, got.State)
	require.Len(t, f.repo.EventsFor(o.ID), 1)
	assert.Equal(t, order.RoutingCancelled, f.repo.EventsFor(o.ID)[0].RoutingKey)
}

func TestScheduledAuditReschedulingRejectsInvalidTimesWithoutMutation(t *testing.T) {
	for _, delta := range []time.Duration{-time.Second, 0, 72 * time.Hour} {
		t.Run(delta.String(), func(t *testing.T) {
			f := newFixture(t)
			o := f.seed(order.StateScheduled)
			at := f.clock.Now().Add(delta)
			_, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
				Version: o.Version, Scheduled: &order.ScheduledChange{At: &at},
			})
			require.Error(t, err)
			assert.Equal(t, order.KindValidation, order.KindOf(err))
			got, err := f.repo.Get(f.ctx, o.ID)
			require.NoError(t, err)
			assert.Equal(t, o, got)
			assert.Empty(t, f.history(o.ID))
			assert.Empty(t, f.credit.Adjusts())
		})
	}
}
