// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The order service: construction, shared helpers and the read side
//   (F3.2, F3.4, F3.5, F3.6, D-045, D-052, D-053).
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Config holds the lifecycle's tunable values (D-047, D-049).
type Config struct {
	ExpiryPeriod  time.Duration // T
	MaxActive     int           // K
	ConfirmWindow time.Duration // C
	// RefundDelay is how long D-047's queued refund waits before the relay may
	// publish it. It must exceed the longest a create request can run.
	RefundDelay time.Duration
}

// DefaultRefundDelay is D-047's 60 seconds.
const DefaultRefundDelay = 60 * time.Second

// Service runs every errand action and query. It is safe for concurrent use.
type Service struct {
	repo      Repository
	credit    CreditClient
	suppliers SupplierDirectory
	clock     Clock
	cfg       Config
	log       *slog.Logger
	// createKeys serializes creates that share a requester and X-Request-Id
	// within this process (D-047).
	createKeys *keyLocks
}

// NewService returns a ready-to-use Service.
func NewService(repo Repository, credit CreditClient, suppliers SupplierDirectory, clock Clock, cfg Config, log *slog.Logger) *Service {
	if cfg.RefundDelay <= 0 {
		cfg.RefundDelay = DefaultRefundDelay
	}
	return &Service{
		repo: repo, credit: credit, suppliers: suppliers, clock: clock, cfg: cfg, log: log,
		createKeys: newKeyLocks(),
	}
}

// Now is the service clock's time, returned to clients as serverTime (D-053).
func (s *Service) Now() time.Time { return s.clock.Now() }

// Scope selects the active or the history half of a list (D-053).
type Scope string

// The two scopes.
const (
	ScopeActive  Scope = "active"
	ScopeHistory Scope = "history"
)

// Get returns an errand and its history, if c may see it (F3.6.2, F3.6.3).
// An errand c may not see is reported as not found.
func (s *Service) Get(ctx context.Context, c Caller, id uuid.UUID) (Order, []HistoryEntry, error) {
	o, err := s.load(ctx, id)
	if err != nil {
		return Order{}, nil, err
	}
	if !CanView(o, c, s.clock.Now()) {
		return Order{}, nil, notFound()
	}
	h, err := s.repo.History(ctx, id)
	if err != nil {
		return Order{}, nil, fmt.Errorf("load history of %s: %w", id, err)
	}
	// AI-generated (edited by James Teo).
	switch RelationshipOf(o, c) {
	case RelNone:
		h = withoutPrivateChanges(h)
	case RelCourier:
		h = withoutNoteValues(h)
	}
	return o, h, nil
}

// withoutPrivateChanges drops every delivery-note change, so a viewer who may
// not see the note cannot read it from an AMEND entry instead.
func withoutPrivateChanges(history []HistoryEntry) []HistoryEntry {
	out := make([]HistoryEntry, 0, len(history))
	for _, h := range history {
		h.Changes = slices.DeleteFunc(slices.Clone(h.Changes), func(fc FieldChange) bool {
			return fc.Field == FieldDeliveryNote
		})
		out = append(out, h)
	}
	return out
}

// AI-generated (edited by James Teo).
// withoutNoteValues keeps every delivery-note change but blanks its values, so
// the courier sees that the note changed and when (F3.7.7), but not a note the
// requester has since replaced or cleared, perhaps one written for an earlier
// courier. The requester and ADMIN keep the values.
func withoutNoteValues(history []HistoryEntry) []HistoryEntry {
	out := make([]HistoryEntry, 0, len(history))
	for _, h := range history {
		h.Changes = slices.Clone(h.Changes)
		for i := range h.Changes {
			if h.Changes[i].Field == FieldDeliveryNote {
				h.Changes[i].From, h.Changes[i].To = "", ""
			}
		}
		out = append(out, h)
	}
	return out
}

// ListOpen lists the errands couriers can accept, newest first (F3.2).
func (s *Service) ListOpen(ctx context.Context, _ Caller) ([]Order, error) {
	out, err := s.repo.ListOpen(ctx, s.clock.Now(), OpenListLimit)
	if err != nil {
		return nil, fmt.Errorf("list open errands: %w", err)
	}
	return out, nil
}

// ListRequested lists c's own errands: those not final, or the final ones (F3.4).
func (s *Service) ListRequested(ctx context.Context, c Caller, scope Scope) ([]Order, error) {
	states, limit := ActiveStates(), AdminListLimit
	if scope == ScopeHistory {
		states, limit = FinalStates(), HistoryListLimit
	}
	out, err := s.repo.ListByRequester(ctx, c.UserID, states, limit)
	if err != nil {
		return nil, fmt.Errorf("list requested errands: %w", err)
	}
	return out, nil
}

// ListAssigned lists the errands c holds as courier and has not finished (F3.5.1).
func (s *Service) ListAssigned(ctx context.Context, c Caller) ([]Order, error) {
	states := []State{StateAccepted, StatePickedUp, StateDelivered}
	out, err := s.repo.ListByCourier(ctx, c.UserID, states, AdminListLimit)
	if err != nil {
		return nil, fmt.Errorf("list assigned errands: %w", err)
	}
	return out, nil
}

// ListAssignmentHistory lists c's ended assignments, released ones included (F3.5.3).
func (s *Service) ListAssignmentHistory(ctx context.Context, c Caller) ([]Assignment, error) {
	out, err := s.repo.ListAssignmentHistory(ctx, c.UserID, HistoryListLimit)
	if err != nil {
		return nil, fmt.Errorf("list assignment history: %w", err)
	}
	return out, nil
}

// ListActive lists every errand that is not final, oldest state change first,
// so an ADMIN sees stalled errands first (D-050, D-053).
func (s *Service) ListActive(ctx context.Context, c Caller) ([]Order, error) {
	if c.Role != RoleAdmin {
		return nil, forbidden("only an admin can list every errand")
	}
	out, err := s.repo.ListActive(ctx, AdminListLimit)
	if err != nil {
		return nil, fmt.Errorf("list active errands: %w", err)
	}
	return out, nil
}

// CountSupplierActive counts the errands that are not final and name the
// supplier, for supplier-service's delete check (D-052, F2.2.3). Only an ADMIN
// may ask: that check runs with the admin's token (D-052).
func (s *Service) CountSupplierActive(ctx context.Context, c Caller, supplierID uuid.UUID) (int, error) {
	if c.Role != RoleAdmin {
		return 0, forbidden("only an admin can count a supplier's errands")
	}
	n, err := s.repo.CountActiveBySupplier(ctx, supplierID)
	if err != nil {
		return 0, fmt.Errorf("count active errands for supplier %s: %w", supplierID, err)
	}
	return n, nil
}

// load reads one errand, turning ErrNotFound into the caller-facing error.
func (s *Service) load(ctx context.Context, id uuid.UUID) (Order, error) {
	o, err := s.repo.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Order{}, notFound()
	}
	if err != nil {
		return Order{}, fmt.Errorf("load errand %s: %w", id, err)
	}
	return o, nil
}

// supplierSnapshot checks a supplier with supplier-service and returns
// the copy an errand keeps (D-052).
func (s *Service) supplierSnapshot(ctx context.Context, id uuid.UUID) (SupplierSnapshot, error) {
	sup, err := s.suppliers.Supplier(ctx, id)
	switch {
	case errors.Is(err, ErrSupplierNotFound):
		return SupplierSnapshot{}, invalid("that supplier does not exist")
	case err != nil:
		return SupplierSnapshot{}, unavailable("supplier-service is not answering; please try again", err)
	case !sup.Available:
		return SupplierSnapshot{}, invalid("that supplier is not taking errands right now")
	}
	return SupplierSnapshot{ID: sup.ID, Name: sup.Name, Location: supplierLocation(sup)}, nil
}

// supplierLocation renders where a supplier is, e.g.
// "Yusof Ishak House, level 1 — Closest to Opp UHC bus stop".
func supplierLocation(s Supplier) string {
	loc := strings.TrimSpace(s.Building)
	if f := strings.TrimSpace(s.Floor); f != "" {
		if loc != "" {
			loc += ", "
		}
		loc += "level " + f
	}
	if d := strings.TrimSpace(s.LocationDescription); d != "" {
		if loc != "" {
			loc += " — "
		}
		loc += d
	}
	return loc
}

func timePtr(t time.Time) *time.Time { return &t }

func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }
