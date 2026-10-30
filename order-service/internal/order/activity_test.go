// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Unit tests for Find errands' shown and search records (D-092).
// Author review: PENDING — <reviewer to complete>

package order_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

var anyFilters = order.Filters{TimeLeft: "any"}

func TestRecordShownStoresEachKnownErrand(t *testing.T) {
	f := newFixture(t)
	a := order.NewActivity(f.repo, f.clock)
	first, second := f.seed(order.StateOpen), f.seed(order.StateOpen)
	filters := order.Filters{TimeLeft: "under1h", From: f.supplier.ID.String(), To: "area:UTown"}

	err := a.RecordShown(f.ctx, f.courier, []order.Shown{
		{OrderID: first.ID, Position: 0},
		{OrderID: uuid.New(), Position: 1}, // not an errand order-service knows
		{OrderID: second.ID, Position: 2},
	}, filters)

	require.NoError(t, err)
	assert.Equal(t, []ordertest.ShownRow{
		{CourierID: f.courier.UserID, Shown: order.Shown{OrderID: first.ID, Position: 0}, Filters: filters, At: f.clock.Now()},
		{CourierID: f.courier.UserID, Shown: order.Shown{OrderID: second.ID, Position: 2}, Filters: filters, At: f.clock.Now()},
	}, f.repo.ShownRows())
}

func TestRecordSearchStoresTheFilters(t *testing.T) {
	f := newFixture(t)
	a := order.NewActivity(f.repo, f.clock)
	filters := order.Filters{TimeLeft: "over1h", To: "place:COM3 Lobby"}

	require.NoError(t, a.RecordSearch(f.ctx, f.courier, filters))

	assert.Equal(t, []ordertest.SearchRow{{CourierID: f.courier.UserID, Filters: filters, At: f.clock.Now()}}, f.repo.SearchRows())
}

func TestActivityRefuses(t *testing.T) {
	f := newFixture(t)
	a := order.NewActivity(f.repo, f.clock)
	one := []order.Shown{{OrderID: f.seed(order.StateOpen).ID}}
	tooMany := make([]order.Shown, order.MaxShownPerRecord+1)
	cases := []struct {
		name    string
		caller  order.Caller
		items   []order.Shown
		filters order.Filters
		want    order.Kind
	}{
		{"an ADMIN", f.admin, one, anyFilters, order.KindForbidden},
		{"no items", f.courier, nil, anyFilters, order.KindValidation},
		{"more items than the open list holds", f.courier, tooMany, anyFilters, order.KindValidation},
		{"a negative position", f.courier, []order.Shown{{OrderID: one[0].OrderID, Position: -1}}, anyFilters, order.KindValidation},
		{"a position past the open list", f.courier, []order.Shown{{OrderID: one[0].OrderID, Position: order.OpenListLimit}}, anyFilters, order.KindValidation},
		{"an unknown time-left filter", f.courier, one, order.Filters{TimeLeft: "soon"}, order.KindValidation},
		{"an empty time-left filter", f.courier, one, order.Filters{}, order.KindValidation},
		{"an over-long from", f.courier, one, order.Filters{TimeLeft: "any", From: strings.Repeat("x", 65)}, order.KindValidation},
		{"an over-long to", f.courier, one, order.Filters{TimeLeft: "any", To: strings.Repeat("x", 201)}, order.KindValidation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireKind(t, a.RecordShown(f.ctx, c.caller, c.items, c.filters), c.want)
		})
	}
	t.Run("a search by an ADMIN or with bad filters", func(t *testing.T) {
		requireKind(t, a.RecordSearch(f.ctx, f.admin, anyFilters), order.KindForbidden)
		requireKind(t, a.RecordSearch(f.ctx, f.courier, order.Filters{TimeLeft: "never"}), order.KindValidation)
	})
	assert.Empty(t, f.repo.ShownRows())
	assert.Empty(t, f.repo.SearchRows())
}

func TestActivityStoreFailuresAreInternal(t *testing.T) {
	f := newFixture(t)
	a := order.NewActivity(f.repo, f.clock)
	f.repo.FailNext("RecordShown", ordertest.ErrInjected)
	f.repo.FailNext("RecordSearch", ordertest.ErrInjected)

	err := a.RecordShown(f.ctx, f.courier, []order.Shown{{OrderID: uuid.New()}}, anyFilters)
	require.ErrorIs(t, err, ordertest.ErrInjected)
	assert.Zero(t, order.KindOf(err))

	err = a.RecordSearch(f.ctx, f.courier, anyFilters)
	require.ErrorIs(t, err, ordertest.ErrInjected)
	assert.Zero(t, order.KindOf(err))
}
