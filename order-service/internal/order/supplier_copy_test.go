// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The supplier copy keeps the supplier's type and position (D-089).
// Author review: PENDING — <reviewer to complete>

package order_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/shuttle"
)

func TestTheSupplierCopyKeepsTypeAndPosition(t *testing.T) {
	f := newFixture(t)
	sup := f.supplier
	sup.Type = "Food/Coffee"
	sup.Point = &shuttle.Point{Lat: 1.2949, Lon: 103.7737}
	f.suppliers.Put(sup)

	o, _, err := f.svc.Create(f.ctx, f.requester, f.createInput())

	require.NoError(t, err)
	assert.Equal(t, "Food/Coffee", o.Supplier.Type)
	assert.Equal(t, sup.Point, o.Supplier.Point)
	assert.Equal(t, o.Supplier, f.stored(o.ID).order.Supplier)
}

func TestAnAmendToAnotherSupplierCopiesItsTypeAndPosition(t *testing.T) {
	f := newFixture(t)
	o := f.post()
	other := f.otherSupplier
	other.Type = "Printing"
	other.Point = nil // supplier-service gave no position
	f.suppliers.Put(other)

	got, err := f.svc.Amend(f.ctx, f.requester, o.ID, order.AmendInput{
		Version: o.Version, SupplierID: &other.ID, CorrelationID: "corr",
	})

	require.NoError(t, err)
	assert.Equal(t, "Printing", got.Supplier.Type)
	assert.Nil(t, got.Supplier.Point)
}
