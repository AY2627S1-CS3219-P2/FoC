// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The domain's small pieces: the seven states and relationships
//   (D-045), the campus location list (D-053), error kinds (D-046) and the
//   correlation id carried in a context (D-047).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"foc/order-service/internal/order"
)

func TestTheSevenStates(t *testing.T) {
	// D-045: seven states, of which COMPLETED, CANCELLED and EXPIRED are final.
	cases := []struct {
		state order.State
		final bool
	}{
		{order.StateOpen, false},
		{order.StateAccepted, false},
		{order.StatePickedUp, false},
		{order.StateDelivered, false},
		{order.StateCompleted, true},
		{order.StateCancelled, true},
		{order.StateExpired, true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.final, tc.state.Final(), "%s", tc.state)
	}
	assert.Equal(t, []order.State{order.StateOpen, order.StateAccepted, order.StatePickedUp, order.StateDelivered},
		order.ActiveStates())
	assert.Equal(t, []order.State{order.StateCompleted, order.StateCancelled, order.StateExpired}, order.FinalStates())
}

func TestRelationshipOf(t *testing.T) {
	requester, courier := uuid.New(), uuid.New()
	accepted := order.Order{RequesterID: requester, CourierID: &courier, State: order.StateAccepted}
	open := order.Order{RequesterID: requester, State: order.StateOpen}

	cases := []struct {
		name   string
		o      order.Order
		caller order.Caller
		want   order.Relationship
	}{
		{"the requester", accepted, order.Caller{UserID: requester, Role: order.RoleStudent}, order.RelRequester},
		{"the assigned courier", accepted, order.Caller{UserID: courier, Role: order.RoleStudent}, order.RelCourier},
		{"another student", accepted, order.Caller{UserID: uuid.New(), Role: order.RoleStudent}, order.RelNone},
		{"a student when nobody is assigned", open, order.Caller{UserID: courier, Role: order.RoleStudent}, order.RelNone},
		{"an admin", accepted, order.Caller{UserID: uuid.New(), Role: order.RoleAdmin}, order.RelAdmin},
		// D-045: admins neither post nor carry errands, so an ADMIN is
		// always the admin, whatever ids the row holds.
		{"an admin whose id the row names", accepted, order.Caller{UserID: requester, Role: order.RoleAdmin}, order.RelAdmin},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, order.RelationshipOf(tc.o, tc.caller), tc.name)
	}
}

func TestDeliveryLocationsAreTheCampusList(t *testing.T) {
	want := []string{
		"COM1 Level 1 study benches",
		"COM3 Lobby",
		"Central Library entrance",
		"UTown Residences lobby",
		"The Deck, AS8",
		"Science Frontier Food Court",
	}
	got := order.DeliveryLocations()
	assert.Equal(t, want, got)

	got[0] = "Somewhere else"
	assert.Equal(t, want, order.DeliveryLocations(), "a caller cannot change the list")
}

func TestErrorKinds(t *testing.T) {
	cause := errors.New("connection refused")
	domain := &order.Error{Kind: order.KindUnavailable, Msg: "credits are unavailable right now", Err: cause}

	assert.Equal(t, order.KindUnavailable, order.KindOf(domain))
	assert.Equal(t, "credits are unavailable right now", order.MessageOf(domain))
	assert.Equal(t, "credits are unavailable right now: connection refused", domain.Error(), "logs see the cause")
	assert.ErrorIs(t, domain, cause)

	wrapped := fmt.Errorf("accept: %w", domain)
	assert.Equal(t, order.KindUnavailable, order.KindOf(wrapped), "found through wrapping")
	assert.Equal(t, "credits are unavailable right now", order.MessageOf(wrapped))

	bare := &order.Error{Kind: order.KindConflict, Msg: "this errand is now accepted"}
	assert.Equal(t, "this errand is now accepted", bare.Error())

	plain := errors.New("disk full")
	assert.Equal(t, order.Kind(0), order.KindOf(plain))
	assert.Equal(t, "internal error", order.MessageOf(plain), "never shown to the caller")
	assert.Equal(t, order.Kind(0), order.KindOf(nil))
}

func TestCorrelationIDTravelsInTheContext(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, order.CorrelationID(ctx))
	assert.Equal(t, "req-42", order.CorrelationID(order.WithCorrelationID(ctx, "req-42")))
}

func TestSystemClockIsUTC(t *testing.T) {
	before := time.Now()
	now := order.SystemClock{}.Now()
	assert.Equal(t, time.UTC, now.Location())
	assert.False(t, now.Before(before.Truncate(time.Second)), "it is the current time")
}
