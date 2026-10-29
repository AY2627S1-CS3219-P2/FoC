// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The model's inputs (D-088, D-096): every feature, measured for one
//   errand at one moment from the courier's history before that moment.
// Author review: PENDING — <reviewer to complete>

package ranking

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
	"foc/order-service/internal/shuttle"
)

// missing is a feature's value when there is nothing to measure it from, such
// as a share of past accepts for a courier who has accepted nothing. Every
// real value of these features is 0 or more, so trees can split it off.
const missing = -1

// The features, in the order of a row. Each is D-088's input as D-096 leaves
// its measuring to the implementation.
const (
	fReward              = iota // the errand's reward
	fAgeMinutes                 // minutes since it was posted
	fTripMetres                 // straight-line metres from pickup to drop-off
	fHour                       // the hour, Singapore time
	fWeekday                    // the weekday, Singapore time, Sunday 0
	fHourShare                  // share of past accepts within an hour of this hour
	fWeekdayShare               // share of past accepts on this weekday
	fCompleted                  // errands the courier completed before
	fTypeShare                  // share of past accepts from this supplier type
	fTypeRating                 // mean stars the courier received on this supplier type
	fRequesterRating            // mean stars the requester received
	fCarrying                   // errands the courier is carrying
	fDetourMetres               // metres from a carried errand's pickup or drop-off to this pickup
	fSharesRoute                // 1 when a shuttle route links a carried errand's drop-off and this pickup
	fSearchedPickup             // share of past searches for this supplier
	fSearchedDestination        // share of past searches that this drop-off matches
	featureCount
)

// FeatureNames name the features in row order. They are saved with each
// model, so a model trained on another list is never scored with this one.
func FeatureNames() []string {
	return []string{
		"reward", "age_minutes", "trip_metres", "hour", "weekday", "hour_share",
		"weekday_share", "completed", "type_share", "type_rating", "requester_rating",
		"carrying", "detour_metres", "shares_route", "searched_pickup", "searched_destination",
	}
}

// inSingapore is t as a Singapore clock reads it. Singapore is UTC+8 all
// year, with no daylight saving, so a fixed shift is exact; the result's hour
// and weekday are Singapore's, though its zone still says UTC.
func inSingapore(t time.Time) time.Time { return t.UTC().Add(8 * time.Hour) }

// Candidate is an errand as the features see it.
type Candidate struct {
	OrderID          uuid.UUID
	RequesterID      uuid.UUID
	Reward           int
	CreatedAt        time.Time
	SupplierID       uuid.UUID
	SupplierName     string
	SupplierType     string
	Pickup           *shuttle.Point
	DeliveryLocation string
}

// CandidateOf is the Candidate for an errand order-service holds.
func CandidateOf(o order.Order) Candidate {
	return Candidate{
		OrderID: o.ID, RequesterID: o.RequesterID, Reward: o.Reward, CreatedAt: o.CreatedAt,
		SupplierID: o.Supplier.ID, SupplierName: o.Supplier.Name, SupplierType: o.Supplier.Type,
		Pickup: o.Supplier.Point, DeliveryLocation: o.DeliveryLocation,
	}
}

// Accept is one errand the courier accepted, and what it was.
type Accept struct {
	OrderID      uuid.UUID
	At           time.Time
	SupplierType string
	// Until is when the courier stopped carrying it: delivered, withdrawn or
	// cancelled. Nil while they still are.
	Until            *time.Time
	Pickup           *shuttle.Point
	DeliveryLocation string
}

// Rating is a rating someone received.
type Rating struct {
	Stars        int
	At           time.Time
	SupplierType string
}

// Search is one change the courier made to Find errands' filters.
type Search struct {
	From string
	To   string
	At   time.Time
}

// History is what the courier did, and what their requesters received, from
// which features are measured as of any moment.
type History struct {
	Accepts     []Accept
	Completions []time.Time
	Ratings     []Rating
	Searches    []Search
	// RequesterRatings are the ratings each requester received.
	RequesterRatings map[uuid.UUID][]Rating
}

// featuresAt measures c for the courier at moment at, from h's events before
// at only: a rating given later cannot explain a choice made earlier.
func featuresAt(c Candidate, at time.Time, h History, places map[string]order.Location) []float64 {
	row := make([]float64, featureCount)
	local := inSingapore(at)
	row[fReward] = float64(c.Reward)
	row[fAgeMinutes] = math.Max(0, at.Sub(c.CreatedAt).Minutes())
	row[fTripMetres] = missing
	drop, haveDrop := places[c.DeliveryLocation]
	if c.Pickup != nil && haveDrop {
		row[fTripMetres] = shuttle.Distance(*c.Pickup, drop.Point)
	}
	row[fHour] = float64(local.Hour())
	row[fWeekday] = float64(local.Weekday())

	var accepts, nearHour, sameDay, sameType int
	for _, a := range h.Accepts {
		if !a.At.Before(at) {
			continue
		}
		accepts++
		l := inSingapore(a.At)
		if hourGap(l.Hour(), local.Hour()) <= 1 {
			nearHour++
		}
		if l.Weekday() == local.Weekday() {
			sameDay++
		}
		if c.SupplierType != "" && a.SupplierType == c.SupplierType {
			sameType++
		}
	}
	row[fHourShare] = share(nearHour, accepts)
	row[fWeekdayShare] = share(sameDay, accepts)
	row[fTypeShare] = missing
	if c.SupplierType != "" {
		row[fTypeShare] = share(sameType, accepts)
	}

	completed := 0
	for _, t := range h.Completions {
		if t.Before(at) {
			completed++
		}
	}
	row[fCompleted] = float64(completed)

	row[fTypeRating] = missing
	if c.SupplierType != "" {
		row[fTypeRating] = meanStars(h.Ratings, at, func(r Rating) bool { return r.SupplierType == c.SupplierType })
	}
	row[fRequesterRating] = meanStars(h.RequesterRatings[c.RequesterID], at, func(Rating) bool { return true })

	carried := carriedAt(h.Accepts, at)
	row[fCarrying] = float64(len(carried))
	row[fDetourMetres] = missing
	row[fSharesRoute] = missing
	if len(carried) > 0 {
		row[fSharesRoute] = 0
		for _, a := range carried {
			if c.Pickup == nil {
				break
			}
			for _, p := range pointsOf(a, places) {
				d := shuttle.Distance(p, *c.Pickup)
				if row[fDetourMetres] == missing || d < row[fDetourMetres] {
					row[fDetourMetres] = d
				}
			}
			if d, ok := places[a.DeliveryLocation]; ok && shareARoute(d.Point, *c.Pickup) {
				row[fSharesRoute] = 1
			}
		}
	}

	var searches, forPickup, forDrop int
	for _, s := range h.Searches {
		if !s.At.Before(at) {
			continue
		}
		searches++
		if s.From != "" && s.From == c.SupplierID.String() {
			forPickup++
		}
		if destinationMatches(s.To, c.DeliveryLocation, drop.Area, haveDrop) {
			forDrop++
		}
	}
	row[fSearchedPickup] = share(forPickup, searches)
	row[fSearchedDestination] = share(forDrop, searches)
	return row
}

// hourGap is the distance between two hours on a 24-hour clock.
func hourGap(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	return min(d, 24-d)
}

func share(n, of int) float64 {
	if of == 0 {
		return missing
	}
	return float64(n) / float64(of)
}

func meanStars(rs []Rating, before time.Time, keep func(Rating) bool) float64 {
	sum, n := 0, 0
	for _, r := range rs {
		if r.At.Before(before) && keep(r) {
			sum += r.Stars
			n++
		}
	}
	if n == 0 {
		return missing
	}
	return float64(sum) / float64(n)
}

// carriedAt returns the errands the courier was carrying at at.
func carriedAt(accepts []Accept, at time.Time) []Accept {
	var out []Accept
	for _, a := range accepts {
		if a.At.Before(at) && (a.Until == nil || a.Until.After(at)) {
			out = append(out, a)
		}
	}
	return out
}

// pointsOf are where a carried errand still takes the courier: its pickup and
// its drop-off, when known.
func pointsOf(a Accept, places map[string]order.Location) []shuttle.Point {
	var out []shuttle.Point
	if a.Pickup != nil {
		out = append(out, *a.Pickup)
	}
	if d, ok := places[a.DeliveryLocation]; ok {
		out = append(out, d.Point)
	}
	return out
}

// shareARoute reports whether one shuttle route serves a stop near a and a stop
// near b, "near" being order.NearbyStopMetres.
func shareARoute(a, b shuttle.Point) bool {
	routes := map[string]bool{}
	for _, s := range shuttle.StopsNear(a, order.NearbyStopMetres) {
		for _, r := range shuttle.RoutesThrough(s.Key) {
			routes[r] = true
		}
	}
	for _, s := range shuttle.StopsNear(b, order.NearbyStopMetres) {
		if slices.ContainsFunc(shuttle.RoutesThrough(s.Key), func(r string) bool { return routes[r] }) {
			return true
		}
	}
	return false
}

// destinationMatches reports whether a search's deliver-to filter takes in the
// errand's drop-off, as Find errands applies it: "place:<name>" or "area:<area>".
func destinationMatches(filter, place, area string, haveArea bool) bool {
	switch {
	case strings.HasPrefix(filter, "place:"):
		return strings.TrimPrefix(filter, "place:") == place
	case strings.HasPrefix(filter, "area:"):
		return haveArea && strings.TrimPrefix(filter, "area:") == area
	}
	return false
}

// placesByName indexes the delivery locations by name.
func placesByName() map[string]order.Location {
	out := map[string]order.Location{}
	for _, l := range order.DeliveryLocations() {
		out[l.Name] = l
	}
	return out
}
