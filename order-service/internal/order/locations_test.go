// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Checks on the delivery locations' coordinates and nearby shuttle
//   stops (D-089, D-097).
// Author review: PENDING — <reviewer to complete>

package order_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"foc/order-service/internal/order"
	"foc/order-service/internal/shuttle"
)

func TestEveryDeliveryLocationIsOnKentRidgeCampus(t *testing.T) {
	for _, l := range order.DeliveryLocations() {
		p := l.Point
		assert.True(t, p.Lat > 1.288 && p.Lat < 1.311 && p.Lon > 103.768 && p.Lon < 103.786,
			"%s at %v is off campus", l.Name, p)
	}
}

func TestNearbyStopsAreWithinRangeNearestFirst(t *testing.T) {
	withStops := 0
	for _, l := range order.DeliveryLocations() {
		near := l.NearbyStops()
		for i, s := range near {
			assert.LessOrEqual(t, s.Metres, float64(order.NearbyStopMetres), l.Name)
			if i > 0 {
				assert.GreaterOrEqual(t, s.Metres, near[i-1].Metres, l.Name)
			}
		}
		if len(near) > 0 {
			withStops++
		}
	}
	// North UTown (RC4, CAPT, NUSC Cendana, the Law Oculus) is 470-550 m from
	// UTown's one shuttle stop, so not every location has a stop this close.
	assert.Greater(t, withStops, 30)
}

func TestEveryDeliveryLocationIsWithin600mOfSomeStop(t *testing.T) {
	// A wider bound than "nearby": a location further than this from every
	// stop would mean a wrong coordinate, not a real gap in the network.
	for _, l := range order.DeliveryLocations() {
		assert.NotEmpty(t, shuttle.StopsNear(l.Point, 600), "%s is over 600 m from every stop", l.Name)
	}
}

func TestTheLT27DeliveryLocationIsTheLT27Stop(t *testing.T) {
	for _, l := range order.DeliveryLocations() {
		if l.Name == "LT27 bus stop" {
			lt27, _ := shuttle.StopByKey("lt27")
			assert.Equal(t, "lt27", l.NearbyStops()[0].Key)
			assert.Zero(t, shuttle.Distance(l.Point, lt27.Point))
			return
		}
	}
	t.Fatal("LT27 bus stop is not a delivery location")
}
