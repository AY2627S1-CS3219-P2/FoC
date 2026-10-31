// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Errand model, caller identity and per-errand relationship (D-045, D-056).
// Author review: James Teo — reviewed and verified.

// Package order is order-service's domain: the errand lifecycle recorded in
// ai/decisions.md D-045 to D-053, D-056 and D-057. It imports no HTTP and no
// SQL (root AGENTS.md §6); the repository, credit and supplier adapters plug
// in through the interfaces in ports.go.
package order

import (
	"time"

	"github.com/google/uuid"
)

// State is an errand's lifecycle state (D-045).
type State string

// The eight states. COMPLETED, CANCELLED and EXPIRED are final.
const (
	// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — recorded state for
	// errands that have a future opening time.
	StateScheduled State = "SCHEDULED"
	StateOpen      State = "OPEN"
	StateAccepted  State = "ACCEPTED"
	StatePickedUp  State = "PICKED_UP"
	StateDelivered State = "DELIVERED"
	StateCompleted State = "COMPLETED"
	StateCancelled State = "CANCELLED"
	StateExpired   State = "EXPIRED"
)

// Final reports whether no action can move an errand out of s.
func (s State) Final() bool {
	return s == StateCompleted || s == StateCancelled || s == StateExpired
}

// ActiveStates are the states that are not final, in lifecycle order.
func ActiveStates() []State {
	return []State{StateOpen, StateAccepted, StatePickedUp, StateDelivered}
}

// FinalStates are the states no action leaves.
func FinalStates() []State {
	return []State{StateCompleted, StateCancelled, StateExpired}
}

// Role is the account role in the access token's role claim (D-019, D-056).
type Role string

// The two roles user-service issues.
const (
	RoleStudent Role = "STUDENT"
	RoleAdmin   Role = "ADMIN"
)

// Caller is the signed-in user behind a request: the sub and role of the
// access token order-service verifies itself (D-056). Requester and courier
// are not roles: they are computed per errand by RelationshipOf (D-021).
type Caller struct {
	UserID uuid.UUID
	Role   Role
}

// Relationship is how a caller relates to one particular errand.
type Relationship string

// The relationships a rule in rules.go can require.
const (
	RelRequester Relationship = "requester"
	RelCourier   Relationship = "courier"
	RelAdmin     Relationship = "admin"
	RelNone      Relationship = "none"
	RelSystem    Relationship = "system"
)

// RelationshipOf works out how c relates to o. An ADMIN is always RelAdmin:
// admins neither post nor carry errands (D-045).
func RelationshipOf(o Order, c Caller) Relationship {
	switch {
	case c.Role == RoleAdmin:
		return RelAdmin
	case c.UserID == o.RequesterID:
		return RelRequester
	case o.CourierID != nil && *o.CourierID == c.UserID:
		return RelCourier
	default:
		return RelNone
	}
}

// SupplierSnapshot is the supplier as it was when the errand was posted or
// last amended (D-052), so a later deactivation or deletion cannot blank it.
type SupplierSnapshot struct {
	ID       uuid.UUID
	Name     string
	Location string
}

// Order is the row order-service stores for an errand (D-053).
type Order struct {
	ID               uuid.UUID
	RequesterID      uuid.UUID
	CourierID        *uuid.UUID
	Supplier         SupplierSnapshot
	ItemDescription  string
	DeliveryLocation string
	DeliveryNote     string
	Reward           int
	State            State
	// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — retains the approved
	// SCHEDULED_TIME value for the service and response mapper.
	ScheduledAt    *time.Time
	Version        int
	RequestKey     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StateChangedAt time.Time
	// ExpiresAt is set only while OPEN (D-049, D-050).
	ExpiresAt *time.Time
	// ConfirmBy is set only while DELIVERED (D-049, D-050).
	ConfirmBy *time.Time
	// AI-generated (edited by James Teo).
	// WithdrawnCouriers are the couriers who withdrew from the errand, read
	// from its history; none of them may accept it again (D-060).
	WithdrawnCouriers []uuid.UUID
}

// ActorKind records who performed a history entry.
type ActorKind string

// The kinds of actor a history entry can name.
const (
	ActorRequester ActorKind = "requester"
	ActorCourier   ActorKind = "courier"
	ActorAdmin     ActorKind = "admin"
	ActorSystem    ActorKind = "system"
)

// Amendable field names, as they appear in a history entry's changes (F3.7.7).
const (
	FieldSupplier         = "supplier"
	FieldItemDescription  = "itemDescription"
	FieldDeliveryLocation = "deliveryLocation"
	FieldDeliveryNote     = "deliveryNote"
	FieldReward           = "reward"
	// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — records requester schedule edits.
	FieldScheduled = "scheduled"
)

// FieldChange is one field an amendment changed (F3.7.7).
type FieldChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// HistoryEntry is one row of order_history: a transition or an amendment
// (D-053, F3.13.1).
type HistoryEntry struct {
	ID        int64
	OrderID   uuid.UUID
	Action    Action
	FromState State // empty for CREATE
	ToState   State
	ActorID   *uuid.UUID // nil for the system
	ActorKind ActorKind
	// CourierID is the courier assigned when the entry was written, so a
	// withdrawn courier's history still finds the errand (F3.5.3).
	CourierID     *uuid.UUID
	Changes       []FieldChange
	OccurredAt    time.Time
	CorrelationID string
}

// AssignmentOutcome is how one courier's assignment ended (F3.5.3).
type AssignmentOutcome string

// The ways an assignment ends.
const (
	OutcomeCompleted AssignmentOutcome = "COMPLETED"
	OutcomeCancelled AssignmentOutcome = "CANCELLED"
	OutcomeReleased  AssignmentOutcome = "RELEASED"
)

// Assignment is one ended assignment in a courier's history (F3.5.3).
type Assignment struct {
	HistoryID        int64
	OrderID          uuid.UUID
	Outcome          AssignmentOutcome
	EndedAt          time.Time
	Supplier         SupplierSnapshot
	ItemDescription  string
	DeliveryLocation string
	Reward           int
}
