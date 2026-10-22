// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The campus delivery-location list and input limits (D-053).
// Author review: James Teo — reviewed and verified.

package order

import (
	"slices"
	"unicode/utf8"
)

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
// Area is the group the form lists it under (D-084).
type Location struct {
	Name string
	Area string
}

// DeliveryLocations is the fixed campus list an errand may deliver to, in the
// order the form shows it, each area's places together (D-053, D-084): public
// spots on Kent Ridge and UTown where a courier can hand an item over. A
// function, not a package-level slice, so callers cannot mutate it.
func DeliveryLocations() []Location {
	return []Location{
		{"Central Library entrance", "Central campus"},
		{"Central Square, YIH", "Central campus"},
		{"University Sports Centre entrance", "Central campus"},
		{"YST Conservatory Foyer", "Central campus"},
		{"The Coffee Roaster, AS8", "Arts & Social Sciences"},
		{"The Deck, FASS", "Arts & Social Sciences"},
		{"BIZ1 Atrium", "Business & Computing"},
		{"COM1 Lobby", "Business & Computing"},
		{"COM3 Lobby", "Business & Computing"},
		{"The Terrace, COM3", "Business & Computing"},
		{"EA Atrium", "Engineering & Design"},
		{"Techno Edge Canteen", "Engineering & Design"},
		{"Frontier Canteen, Science", "Science & Medicine"},
		{"LT27 bus stop", "Science & Medicine"},
		{"Medicine+Science Library Atrium", "Science & Medicine"},
		{"Fine Food, Town Plaza", "UTown"},
		{"Flavours, Stephen Riady Centre", "UTown"},
		{"NUS Law Oculus drop-off", "UTown"},
		{"Starbucks, UTown ERC", "UTown"},
		{"Acacia College lobby", "UTown residences"},
		{"CAPT lobby", "UTown residences"},
		{"NUSC Cendana lobby", "UTown residences"},
		{"NUSC Elm lobby", "UTown residences"},
		{"NUSC Saga lobby", "UTown residences"},
		{"RC4 lobby", "UTown residences"},
		{"Tembusu College lobby", "UTown residences"},
		{"UTown Residence lobby", "UTown residences"},
		{"Eusoff Hall lobby", "Kent Ridge residences"},
		{"Kent Ridge Hall lobby", "Kent Ridge residences"},
		{"KEVII Hall lobby", "Kent Ridge residences"},
		{"Raffles Hall lobby", "Kent Ridge residences"},
		{"RVRC lobby", "Kent Ridge residences"},
		{"Sheares Hall lobby", "Kent Ridge residences"},
		{"Temasek Hall lobby", "Kent Ridge residences"},
		{"Valour House lobby", "Kent Ridge residences"},
		{"Helix House lobby", "Prince George's Park"},
		{"LightHouse lobby", "Prince George's Park"},
		{"PGP Aircon Canteen", "Prince George's Park"},
		{"PGP Residences lobby", "Prince George's Park"},
		{"Pioneer House lobby", "Prince George's Park"},
	}
}

func isDeliveryLocation(s string) bool {
	return slices.ContainsFunc(DeliveryLocations(), func(l Location) bool { return l.Name == s })
}

func charLen(s string) int { return utf8.RuneCountInString(s) }
