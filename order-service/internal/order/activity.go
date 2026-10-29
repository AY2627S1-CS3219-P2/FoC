// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The records Find errands sends for errand ranking (D-088, D-092):
//   which errands a courier was shown, and the filters they set.
// Author review: PENDING — <reviewer to complete>

package order

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MaxShownPerRecord caps how many errands one shown record may carry: the open
// list holds at most OpenListLimit.
const MaxShownPerRecord = OpenListLimit

// The longest From and To filter values accepted, in bytes.
const (
	maxFilterFrom = 64
	maxFilterTo   = 200
)

// Filters are the filters set on Find errands when a record was made (D-092).
type Filters struct {
	TimeLeft string
	From     string
	To       string
}

// Shown is one errand a courier saw, and its place on the list (0 is the top).
type Shown struct {
	OrderID  uuid.UUID
	Position int
}

// ActivityStore keeps the records, implemented by internal/repository and by
// fakes in tests.
type ActivityStore interface {
	// RecordShown stores one row per item. An item naming an errand the store
	// does not know is skipped.
	RecordShown(ctx context.Context, courierID uuid.UUID, items []Shown, f Filters, at time.Time) error
	// RecordSearch stores the filters a courier set.
	RecordSearch(ctx context.Context, courierID uuid.UUID, f Filters, at time.Time) error
}

// Activity records what couriers do on Find errands. It is safe for concurrent
// use.
type Activity struct {
	store ActivityStore
	clock Clock
}

// NewActivity returns a ready-to-use Activity.
func NewActivity(store ActivityStore, clock Clock) *Activity {
	return &Activity{store: store, clock: clock}
}

// RecordShown records that c, as a courier, saw items on Find errands.
func (a *Activity) RecordShown(ctx context.Context, c Caller, items []Shown, f Filters) error {
	if c.Role != RoleStudent {
		return forbidden("only students browse errands as couriers")
	}
	if len(items) == 0 || len(items) > MaxShownPerRecord {
		return invalid(fmt.Sprintf("items must hold 1 to %d errands", MaxShownPerRecord))
	}
	for _, it := range items {
		// A position is a place on the open list, which holds at most
		// OpenListLimit errands.
		if it.Position < 0 || it.Position >= OpenListLimit {
			return invalid(fmt.Sprintf("position must be from 0 to %d", OpenListLimit-1))
		}
	}
	if err := validateFilters(f); err != nil {
		return err
	}
	if err := a.store.RecordShown(ctx, c.UserID, items, f, a.clock.Now()); err != nil {
		return fmt.Errorf("record %d shown errands for %s: %w", len(items), c.UserID, err)
	}
	return nil
}

// RecordSearch records the filters c set on Find errands.
func (a *Activity) RecordSearch(ctx context.Context, c Caller, f Filters) error {
	if c.Role != RoleStudent {
		return forbidden("only students browse errands as couriers")
	}
	if err := validateFilters(f); err != nil {
		return err
	}
	if err := a.store.RecordSearch(ctx, c.UserID, f, a.clock.Now()); err != nil {
		return fmt.Errorf("record search for %s: %w", c.UserID, err)
	}
	return nil
}

// isTimeLeftFilter reports whether s is one of Find errands' time-left choices,
// as the screen names them.
func isTimeLeftFilter(s string) bool {
	switch s {
	case "any", "under30m", "under1h", "over1h":
		return true
	}
	return false
}

func validateFilters(f Filters) error {
	switch {
	case !isTimeLeftFilter(f.TimeLeft):
		return invalid("filters.timeLeft must be one of any, under30m, under1h, over1h")
	case len(f.From) > maxFilterFrom:
		return invalid(fmt.Sprintf("filters.from must be at most %d characters", maxFilterFrom))
	case len(f.To) > maxFilterTo:
		return invalid(fmt.Sprintf("filters.to must be at most %d characters", maxFilterTo))
	}
	return nil
}
