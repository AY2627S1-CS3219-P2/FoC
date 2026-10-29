// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Unit tests for the shuttle data and its lookups: the data hangs
//   together, distances match known values, and lookups sort and filter.
// Author review: PENDING — <reviewer to complete>

package shuttle_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/shuttle"
)

func TestEveryRouteStopIsAKnownStop(t *testing.T) {
	for _, r := range shuttle.Routes() {
		require.GreaterOrEqual(t, len(r.Stops), 2, r.Code)
		for _, key := range r.Stops {
			_, ok := shuttle.StopByKey(key)
			assert.True(t, ok, "route %s names unknown stop %q", r.Code, key)
		}
		assert.NotEmpty(t, r.Hours, r.Code)
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, r.Updated, r.Code)
	}
}

func TestStopKeysCodesAndRouteCodesAreUnique(t *testing.T) {
	keys, codes, routes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, s := range shuttle.Stops() {
		assert.False(t, keys[s.Key], "duplicate stop key %q", s.Key)
		keys[s.Key] = true
		if s.Code != "" {
			assert.False(t, codes[s.Code], "duplicate LTA code %q", s.Code)
			codes[s.Code] = true
		}
	}
	for _, r := range shuttle.Routes() {
		assert.False(t, routes[r.Code], "duplicate route %q", r.Code)
		routes[r.Code] = true
	}
}

func TestEveryStopIsUsedAndInSingapore(t *testing.T) {
	for _, s := range shuttle.Stops() {
		assert.NotEmpty(t, shuttle.RoutesThrough(s.Key), "stop %q is on no route", s.Key)
		assert.True(t, s.Lat > 1.2 && s.Lat < 1.5 && s.Lon > 103.6 && s.Lon < 104.1,
			"stop %q at %v is not in Singapore", s.Key, s.Point)
	}
}

func TestDistanceMatchesKnownValues(t *testing.T) {
	a := shuttle.Point{Lat: 1.3, Lon: 103.77}
	assert.Zero(t, shuttle.Distance(a, a))
	// One degree of latitude on a sphere of the mean Earth radius.
	assert.InDelta(t, 111_195, shuttle.Distance(shuttle.Point{}, shuttle.Point{Lat: 1}), 1)
	b := shuttle.Point{Lat: 1.31, Lon: 103.78}
	assert.InDelta(t, shuttle.Distance(a, b), shuttle.Distance(b, a), 1e-9)

	com3, _ := shuttle.StopByKey("com3")
	biz2, _ := shuttle.StopByKey("biz2")
	// The COM 3 and BIZ 2 stops' pins are about 190 m apart.
	assert.InDelta(t, 190, shuttle.Distance(com3.Point, biz2.Point), 20)
}

func TestStopsNearIsSortedAndWithinRange(t *testing.T) {
	com3, ok := shuttle.StopByKey("com3")
	require.True(t, ok)

	near := shuttle.StopsNear(com3.Point, 400)
	require.NotEmpty(t, near)
	assert.Equal(t, "com3", near[0].Key)
	assert.Zero(t, near[0].Metres)
	for i, s := range near {
		assert.LessOrEqual(t, s.Metres, 400.0)
		if i > 0 {
			assert.GreaterOrEqual(t, s.Metres, near[i-1].Metres)
		}
	}
	assert.Empty(t, shuttle.StopsNear(shuttle.Point{Lat: 1.35, Lon: 103.95}, 1000), "Tampines has no NUS stop")
}

func TestLookupsByKey(t *testing.T) {
	_, ok := shuttle.StopByKey("nowhere")
	assert.False(t, ok)
	assert.Equal(t, []string{"D1", "D2"}, shuttle.RoutesThrough("com3"))
	assert.Empty(t, shuttle.RoutesThrough("nowhere"))
}

func TestCallersCannotChangeTheData(t *testing.T) {
	shuttle.Stops()[0].Name = "changed"
	shuttle.Routes()[0].Stops[0] = "changed"
	assert.NotEqual(t, "changed", shuttle.Stops()[0].Name)
	assert.NotEqual(t, "changed", shuttle.Routes()[0].Stops[0])
}
