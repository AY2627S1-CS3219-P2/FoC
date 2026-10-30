// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The NUS shuttle data's types and lookups (D-090, D-097): stops,
//   routes, straight-line distance, and the stops near a point.
// Author review: PENDING — <reviewer to complete>

// Package shuttle is the static NUS Internal Shuttle Bus data errand ranking
// reads (D-090): every stop with its position, every route's stops in order,
// and where and when the data was taken from (D-097). Nothing here calls NUS
// or any other service; when NUS changes a route, data.go is edited and the
// as-of date moved.
package shuttle

import (
	"cmp"
	"math"
	"slices"
)

// earthRadius is the mean radius of the Earth in metres.
const earthRadius = 6_371_008.8

// Point is a position in degrees (WGS 84).
type Point struct {
	Lat float64
	Lon float64
}

// Stop is one shuttle stop.
type Stop struct {
	// Key names the stop inside this package's data.
	Key string
	// Name is the stop's name as the source lists it.
	Name string
	// Code is the LTA bus stop code, or empty for a stop on NUS roads that
	// has none.
	Code string
	Point
	// Approximate says why the position is not the stop's own pin, or is
	// empty when it is.
	Approximate string
}

// Route is one shuttle service.
type Route struct {
	// Code is the service name on the bus, such as "A1".
	Code string
	// Loop is true when the last stop leads back to the first.
	Loop bool
	// Stops are the keys of the stops served, in order.
	Stops []string
	// Hours are the operating hours as the source states them.
	Hours string
	// Updated is when the source last updated this route's page.
	Updated string
}

// StopDistance is a stop and how far it is from the point asked about.
type StopDistance struct {
	Stop
	Metres float64
}

// Distance is the great-circle distance between a and b in metres.
func Distance(a, b Point) float64 {
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLon := (b.Lon - a.Lon) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}

// StopsNear returns the stops within metres of p, nearest first.
func StopsNear(p Point, metres float64) []StopDistance {
	var near []StopDistance
	for _, s := range Stops() {
		if d := Distance(p, s.Point); d <= metres {
			near = append(near, StopDistance{Stop: s, Metres: d})
		}
	}
	slices.SortFunc(near, func(a, b StopDistance) int {
		return cmp.Or(cmp.Compare(a.Metres, b.Metres), cmp.Compare(a.Key, b.Key))
	})
	return near
}

// StopByKey returns the stop with key, and false when there is none.
func StopByKey(key string) (Stop, bool) {
	stops := Stops()
	i := slices.IndexFunc(stops, func(s Stop) bool { return s.Key == key })
	if i < 0 {
		return Stop{}, false
	}
	return stops[i], true
}

// RoutesThrough returns the codes of the routes that serve the stop with key.
func RoutesThrough(key string) []string {
	var codes []string
	for _, r := range Routes() {
		if slices.Contains(r.Stops, key) {
			codes = append(codes, r.Code)
		}
	}
	return codes
}
