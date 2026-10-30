// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The campus delivery-location list and input limits (D-053).
// Author review: James Teo — reviewed and verified.
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Each location's coordinates, looked up on 2026-10-29, and the
//   shuttle stops near it (D-089, D-097).
// Author review: PENDING — <reviewer to complete>

package order

import (
	"slices"
	"unicode/utf8"

	"foc/order-service/internal/shuttle"
)

// NearbyStopMetres is how far a shuttle stop may be from a delivery location
// and still count as near it: about five minutes' walk.
const NearbyStopMetres = 400

// Input limits, counted in characters (D-053).
const (
	MaxItemDescription = 280
	MaxDeliveryNote    = 140
	// MaxReward is the largest reward credit-service holds, so a larger one is
	// refused here with a 400 rather than by credit-service (D-082). F3.1.2
	// sets only the lower bound.
	MaxReward = 1_000_000
)

// Location is a campus place an errand may deliver to. An errand stores Name;
// Area is the group the form lists it under (D-084). Point is where it is
// (D-089); only errand ranking reads it, and GET /locations does not send it.
type Location struct {
	Name  string
	Area  string
	Point shuttle.Point
}

// NearbyStops returns the shuttle stops within NearbyStopMetres of l, nearest
// first (D-089).
func (l Location) NearbyStops() []shuttle.StopDistance {
	return shuttle.StopsNear(l.Point, NearbyStopMetres)
}

// DeliveryLocations is the fixed campus list an errand may deliver to, in the
// order the form shows it, each area's places together (D-053, D-084): public
// spots on Kent Ridge and UTown where a courier can hand an item over. A
// function, not a package-level slice, so callers cannot mutate it.
//
// Points are OpenStreetMap's (© OpenStreetMap contributors, ODbL), read
// through the Overpass API on 2026-10-29 (D-097). Most are the named
// building's centre, not its door, so they are good to some tens of metres.
// The Coffee Roaster, The Terrace, Techno Edge Canteen and the three NUSC
// colleges are OSM's own pins for those places; LT27 bus stop is LTA stop
// 18301; Sheares Hall is its mapped entrance. NUS Law Oculus has no OSM entry:
// NUS Law's directions put it a minute or two downhill from bus stop 17099
// (UTown – Cendana), so it shares NUSC Cendana's point and is approximate.
func DeliveryLocations() []Location {
	return []Location{
		{"Central Library entrance", "Central campus", shuttle.Point{Lat: 1.296621, Lon: 103.773076}},
		{"Central Square, YIH", "Central campus", shuttle.Point{Lat: 1.298515, Lon: 103.775480}},
		{"University Sports Centre entrance", "Central campus", shuttle.Point{Lat: 1.299711, Lon: 103.775458}},
		{"YST Conservatory Foyer", "Central campus", shuttle.Point{Lat: 1.302183, Lon: 103.772647}},
		{"The Coffee Roaster, AS8", "Arts & Social Sciences", shuttle.Point{Lat: 1.296068, Lon: 103.772008}},
		{"The Deck, FASS", "Arts & Social Sciences", shuttle.Point{Lat: 1.294671, Lon: 103.772492}},
		{"BIZ1 Atrium", "Business & Computing", shuttle.Point{Lat: 1.292502, Lon: 103.774146}},
		{"COM1 Lobby", "Business & Computing", shuttle.Point{Lat: 1.295016, Lon: 103.773909}},
		{"COM3 Lobby", "Business & Computing", shuttle.Point{Lat: 1.294583, Lon: 103.774582}},
		{"The Terrace, COM3", "Business & Computing", shuttle.Point{Lat: 1.294523, Lon: 103.774475}},
		{"EA Atrium", "Engineering & Design", shuttle.Point{Lat: 1.300249, Lon: 103.770694}},
		{"Techno Edge Canteen", "Engineering & Design", shuttle.Point{Lat: 1.297946, Lon: 103.771515}},
		{"Frontier Canteen, Science", "Science & Medicine", shuttle.Point{Lat: 1.296304, Lon: 103.780068}},
		{"LT27 bus stop", "Science & Medicine", shuttle.Point{Lat: 1.297406, Lon: 103.780990}},
		{"Medicine+Science Library Atrium", "Science & Medicine", shuttle.Point{Lat: 1.296958, Lon: 103.781734}},
		{"Fine Food, Town Plaza", "UTown", shuttle.Point{Lat: 1.303934, Lon: 103.773647}},
		{"Flavours, Stephen Riady Centre", "UTown", shuttle.Point{Lat: 1.304508, Lon: 103.772806}},
		{"NUS Law Oculus drop-off", "UTown", shuttle.Point{Lat: 1.307863, Lon: 103.772306}},
		{"Starbucks, UTown ERC", "UTown", shuttle.Point{Lat: 1.305549, Lon: 103.772851}},
		{"Acacia College lobby", "UTown residences", shuttle.Point{Lat: 1.306782, Lon: 103.773308}},
		{"CAPT lobby", "UTown residences", shuttle.Point{Lat: 1.307599, Lon: 103.773148}},
		{"NUSC Cendana lobby", "UTown residences", shuttle.Point{Lat: 1.307863, Lon: 103.772306}},
		{"NUSC Elm lobby", "UTown residences", shuttle.Point{Lat: 1.306391, Lon: 103.772376}},
		{"NUSC Saga lobby", "UTown residences", shuttle.Point{Lat: 1.305684, Lon: 103.772176}},
		{"RC4 lobby", "UTown residences", shuttle.Point{Lat: 1.308330, Lon: 103.773172}},
		{"Tembusu College lobby", "UTown residences", shuttle.Point{Lat: 1.306041, Lon: 103.773683}},
		{"UTown Residence lobby", "UTown residences", shuttle.Point{Lat: 1.305130, Lon: 103.773861}},
		{"Eusoff Hall lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.293587, Lon: 103.770041}},
		{"Kent Ridge Hall lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.291601, Lon: 103.774536}},
		{"KEVII Hall lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.292464, Lon: 103.780688}},
		{"Raffles Hall lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.300083, Lon: 103.773315}},
		{"RVRC lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.298162, Lon: 103.776333}},
		{"Sheares Hall lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.291459, Lon: 103.775546}},
		{"Temasek Hall lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.292869, Lon: 103.771537}},
		{"Valour House lobby", "Kent Ridge residences", shuttle.Point{Lat: 1.300511, Lon: 103.774880}},
		{"Helix House lobby", "Prince George's Park", shuttle.Point{Lat: 1.291192, Lon: 103.779695}},
		{"LightHouse lobby", "Prince George's Park", shuttle.Point{Lat: 1.290442, Lon: 103.781786}},
		{"PGP Aircon Canteen", "Prince George's Park", shuttle.Point{Lat: 1.290745, Lon: 103.780810}},
		{"PGP Residences lobby", "Prince George's Park", shuttle.Point{Lat: 1.290965, Lon: 103.781220}},
		{"Pioneer House lobby", "Prince George's Park", shuttle.Point{Lat: 1.290525, Lon: 103.780263}},
	}
}

func isDeliveryLocation(s string) bool {
	return slices.ContainsFunc(DeliveryLocations(), func(l Location) bool { return l.Name == s })
}

func charLen(s string) int { return utf8.RuneCountInString(s) }
