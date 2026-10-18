// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The transition table (D-045) and the per-action checks (D-046).
// Author review: James Teo — reviewed and verified.

package order

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Action is a lifecycle action; history entries record it (D-045).
type Action string

// Every action in D-045's table, plus CREATE.
const (
	ActionCreate        Action = "CREATE"
	ActionAmend         Action = "AMEND"
	ActionAccept        Action = "ACCEPT"
	ActionWithdraw      Action = "WITHDRAW"
	ActionPickUp        Action = "PICK_UP"
	ActionDeliver       Action = "DELIVER"
	ActionConfirm       Action = "CONFIRM"
	ActionAutoConfirm   Action = "AUTO_CONFIRM"
	ActionCancel        Action = "CANCEL"
	ActionExpire        Action = "EXPIRE"
	ActionAdminCancel   Action = "ADMIN_CANCEL"
	ActionAdminComplete Action = "ADMIN_COMPLETE"
)

// rule is one row of D-045's table: the states an action may start from, the
// state it ends in, and who may run it.
type rule struct {
	from []State
	to   State
	who  Relationship
}

// ruleFor returns D-045's row for a. CREATE has no row: it starts no errand
// from a state. A switch, not a package-level map, so the table cannot be
// mutated at run time (root AGENTS.md §5).
func ruleFor(a Action) (rule, bool) {
	switch a {
	case ActionAmend:
		return rule{from: []State{StateOpen}, to: StateOpen, who: RelRequester}, true
	case ActionAccept:
		// Any STUDENT who is not the requester: an ADMIN is RelAdmin and the
		// requester is RelRequester, so RelNone plus the role check covers it.
		return rule{from: []State{StateOpen}, to: StateAccepted, who: RelNone}, true
	case ActionWithdraw:
		return rule{from: []State{StateAccepted}, to: StateOpen, who: RelCourier}, true
	case ActionPickUp:
		return rule{from: []State{StateAccepted}, to: StatePickedUp, who: RelCourier}, true
	case ActionDeliver:
		return rule{from: []State{StatePickedUp}, to: StateDelivered, who: RelCourier}, true
	case ActionConfirm:
		return rule{from: []State{StateDelivered}, to: StateCompleted, who: RelRequester}, true
	case ActionAutoConfirm:
		return rule{from: []State{StateDelivered}, to: StateCompleted, who: RelSystem}, true
	case ActionCancel:
		return rule{from: []State{StateOpen}, to: StateCancelled, who: RelRequester}, true
	case ActionExpire:
		return rule{from: []State{StateOpen}, to: StateExpired, who: RelSystem}, true
	case ActionAdminCancel:
		return rule{from: ActiveStates(), to: StateCancelled, who: RelAdmin}, true
	case ActionAdminComplete:
		return rule{from: []State{StatePickedUp, StateDelivered}, to: StateCompleted, who: RelAdmin}, true
	default:
		return rule{}, false
	}
}

// actor is whoever is running an action: a signed-in caller, or the sweeper.
type actor struct {
	caller *Caller // nil for the system
}

func userActor(c Caller) actor { return actor{caller: &c} }

func systemActor() actor { return actor{} }

func (a actor) relationship(o Order) Relationship {
	if a.caller == nil {
		return RelSystem
	}
	return RelationshipOf(o, *a.caller)
}

// check runs D-046's checks for one action, in the order that fixes the status
// code: relationship (403), then state (409), then deadlines (409). Version and
// K are checked where the write happens. It returns nil when a may run now.
func check(o Order, a Action, by actor, now time.Time) error {
	r, ok := ruleFor(a)
	if !ok {
		return fmt.Errorf("check: no rule for action %q", a)
	}
	if err := checkWho(o, a, r, by); err != nil {
		return err
	}
	if !slices.Contains(r.from, o.State) {
		if a == ActionAccept {
			// Only a student with no part in the errand gets this far on an
			// accept, and they may no longer see an errand that has left OPEN
			// (CanView, D-045, F3.6.2), so the answer must not name its state.
			return conflict("this errand is no longer open")
		}
		return stateConflict(o.State)
	}
	return checkDeadline(o, a, now)
}

func checkWho(o Order, a Action, r rule, by actor) error {
	rel := by.relationship(o)
	if rel == r.who {
		if a == ActionAccept && by.caller.Role != RoleStudent {
			return forbidden("only students can accept errands")
		}
		return nil
	}
	switch {
	case a == ActionAccept && rel == RelRequester:
		return forbidden("you cannot accept your own errand")
	case a == ActionAccept && rel == RelCourier:
		// Re-tapping Accept on an errand you already hold.
		return stateConflict(o.State)
	case r.who == RelRequester:
		return forbidden("only the errand's requester can do this")
	case r.who == RelCourier:
		return forbidden("only the errand's assigned courier can do this")
	case r.who == RelAdmin:
		return forbidden("only an admin can do this")
	default:
		return forbidden("you cannot do this to this errand")
	}
}

// checkDeadline enforces D-049: user actions check the deadlines too, so a
// passed deadline means what the sweeper will make of it even before the sweep
// runs. Every action on an OPEN errand except Expire needs it before its
// deadline T; an admin cancel of a DELIVERED errand needs it before C, because
// once C has passed the errand completes and the courier is paid (F3.9.4).
// Confirm and admin complete stay allowed past C: they reach the same outcome.
// The system actions run only once their deadline has passed.
func checkDeadline(o Order, a Action, now time.Time) error {
	switch a {
	case ActionExpire:
		if o.ExpiresAt == nil || now.Before(*o.ExpiresAt) {
			return ErrNotDue
		}
	case ActionAutoConfirm:
		if o.ConfirmBy == nil || now.Before(*o.ConfirmBy) {
			return ErrNotDue
		}
	default:
		if o.State == StateOpen && o.ExpiresAt != nil && !now.Before(*o.ExpiresAt) {
			return conflict("this errand has expired")
		}
		if a == ActionAdminCancel && o.State == StateDelivered && o.ConfirmBy != nil && !now.Before(*o.ConfirmBy) {
			return conflict("the confirmation window has passed; this errand is being completed")
		}
	}
	return nil
}

func stateConflict(s State) error {
	return conflict("this errand is now " + strings.ToLower(strings.ReplaceAll(string(s), "_", " ")))
}

// APIAction is an action as the API names it in an errand's actions list and
// in the route POST /{id}/<name> (D-053).
type APIAction string

// The API's action names. "cancel" maps to the requester's Cancel or an
// ADMIN's admin cancel, by the caller's relationship to the errand.
const (
	APIAmend    APIAction = "amend"
	APIAccept   APIAction = "accept"
	APIWithdraw APIAction = "withdraw"
	APIPickUp   APIAction = "pickup"
	APIDeliver  APIAction = "deliver"
	APIConfirm  APIAction = "confirm"
	APICancel   APIAction = "cancel"
	APIComplete APIAction = "complete"
)

// apiActions lists, for each API name, the domain actions it can mean.
func apiActions() []struct {
	name    APIAction
	actions []Action
} {
	return []struct {
		name    APIAction
		actions []Action
	}{
		{APIAmend, []Action{ActionAmend}},
		{APIAccept, []Action{ActionAccept}},
		{APIWithdraw, []Action{ActionWithdraw}},
		{APIPickUp, []Action{ActionPickUp}},
		{APIDeliver, []Action{ActionDeliver}},
		{APIConfirm, []Action{ActionConfirm}},
		{APICancel, []Action{ActionCancel, ActionAdminCancel}},
		{APIComplete, []Action{ActionAdminComplete}},
	}
}

// AllowedActions lists what c may do to o at now, so the frontend shows one
// button per entry and never re-implements the table (D-053). K and the version
// are checked only when the action runs.
func AllowedActions(o Order, c Caller, now time.Time) []APIAction {
	out := []APIAction{}
	for _, entry := range apiActions() {
		for _, a := range entry.actions {
			if check(o, a, userActor(c), now) == nil {
				out = append(out, entry.name)
				break
			}
		}
	}
	return out
}

// CanView reports whether c may see o at all (F3.6.2, F3.6.3, D-045): its
// requester, its courier and ADMIN always; anyone else only while it is OPEN
// and before its deadline.
func CanView(o Order, c Caller, now time.Time) bool {
	if RelationshipOf(o, c) != RelNone {
		return true
	}
	return o.State == StateOpen && o.ExpiresAt != nil && now.Before(*o.ExpiresAt)
}

// CanSeePrivate reports whether c may see o's requester and delivery note:
// only a party to the errand or an ADMIN.
func CanSeePrivate(o Order, c Caller) bool {
	return RelationshipOf(o, c) != RelNone
}
