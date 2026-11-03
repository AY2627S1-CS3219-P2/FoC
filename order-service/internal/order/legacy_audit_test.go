// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-10-28
// Scope: Characterizes the pre-existing supplier-check/create race using the
//   domain's dependency fakes; no implementation or contract changes.
// Author review: PENDING

package order_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func TestLegacyAuditSupplierCheckHasGapBeforeCreateCommit(t *testing.T) {
	f := newFixture(t)
	f.credit.ReserveHook = func(order.Reservation) (bool, error) {
		count, err := f.svc.CountSupplierActive(f.ctx, f.admin, f.supplier.ID)
		require.NoError(t, err)
		require.Zero(t, count, "the supplier deletion check cannot yet see this in-flight create")
		// Model deletion after the successful lookup but before saving.
		f.suppliers.FailWith(order.ErrSupplierNotFound)
		return true, nil
	}
	o, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())
	require.NoError(t, err, "current behavior saves the earlier snapshot after supplier disappearance")
	require.Equal(t, order.StateOpen, o.State)
	count, err := f.svc.CountSupplierActive(f.ctx, f.admin, f.supplier.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
