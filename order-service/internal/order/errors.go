// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Domain error kinds and the sentinel errors adapters return (D-046).
// Author review: James Teo — reviewed and verified.

package order

import (
	"errors"
	"fmt"
)

// Kind classifies a domain error so the transport can pick a status code
// without knowing the business rule behind it (root AGENTS.md §6).
type Kind int

// The kinds of failure the service reports.
const (
	KindValidation Kind = iota + 1
	KindForbidden
	KindNotFound
	KindConflict
	KindInsufficientCredits
	KindUnavailable
)

// Error is a failure the caller can act on, with a message safe to show.
type Error struct {
	Kind Kind
	Msg  string
	Err  error // the cause, for logs; never shown to the caller
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf returns err's Kind, or 0 when err is not a domain *Error.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

// MessageOf returns the caller-safe message of a domain error.
func MessageOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return "internal error"
}

func invalid(msg string) error   { return &Error{Kind: KindValidation, Msg: msg} }
func forbidden(msg string) error { return &Error{Kind: KindForbidden, Msg: msg} }
func conflict(msg string) error  { return &Error{Kind: KindConflict, Msg: msg} }

func notFound() error { return &Error{Kind: KindNotFound, Msg: "errand not found"} }

func insufficientCredits(cause error) error {
	return &Error{Kind: KindInsufficientCredits, Msg: "not enough available credits for this reward", Err: cause}
}

func unavailable(msg string, cause error) error {
	return &Error{Kind: KindUnavailable, Msg: msg, Err: cause}
}

// Sentinel errors returned by the ports in ports.go. The service turns them
// into *Error values; nothing outside this package should see them raw.
var (
	// ErrNotFound: no errand has that id.
	ErrNotFound = errors.New("errand not found")
	// ErrNoMatch: a conditional write matched no row because the errand
	// changed after it was read (D-046).
	ErrNoMatch = errors.New("errand changed before the write")
	// ErrCourierAtCapacity: the courier already holds K active assignments (D-046).
	ErrCourierAtCapacity = errors.New("courier holds the maximum number of active errands")
	// ErrDuplicateRequestKey: the requester already created an errand with this
	// X-Request-Id (D-047).
	ErrDuplicateRequestKey = errors.New("request key already used")
	// ErrGuardGone: the queued refund was already published, so the errand must
	// not be saved (D-047).
	ErrGuardGone = errors.New("queued refund already published")
	// AI-generated (edited by James Teo).
	// ErrTooManyOpen: saving the errand would give the requester more than
	// MaxOpenErrands errands OPEN before their deadlines (D-058).
	ErrTooManyOpen = errors.New("requester has the most open errands allowed")
	// ErrNotDue: a system action's deadline has not passed yet.
	ErrNotDue = errors.New("deadline not reached")

	// ErrSupplierNotFound: supplier-service has no such supplier, or it was deleted.
	ErrSupplierNotFound = errors.New("supplier not found")

	// ErrInsufficientCredits: credit-service answered 422 (F4.2.1, F4.5.3).
	ErrInsufficientCredits = errors.New("insufficient credits")
	// ErrCreditRejected: credit-service gave a definite refusal other than 422;
	// nothing was applied.
	ErrCreditRejected = errors.New("credit-service rejected the request")
)
