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
	// MaxReward keeps a reward inside the database's INTEGER column; F3.1.2
	// sets only the lower bound.
	MaxReward = 2147483647
)

// DeliveryLocations is the fixed campus list an errand may deliver to (D-053).
// A function, not a package-level slice, so callers cannot mutate it.
func DeliveryLocations() []string {
	return []string{
		"COM1 Level 1 study benches",
		"COM3 Lobby",
		"Central Library entrance",
		"UTown Residences lobby",
		"The Deck, AS8",
		"Science Frontier Food Court",
	}
}

func isDeliveryLocation(s string) bool {
	return slices.Contains(DeliveryLocations(), s)
}

func charLen(s string) int { return utf8.RuneCountInString(s) }
