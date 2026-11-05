// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: A malformed id is ErrNotFound before any repository or order-service call,
//   every repository call gets the canonical lower-case id, and Create keeps an
//   explicit is_available: false instead of overriding it with true.
// Author review: PENDING — <reviewer to complete>

package supplier

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var timePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// AI-generated (edited by James Teo).
// ErrandCounter reports how many errands that are not finished yet reference
// a supplier. Errands belong to order-service, so the implementation asks it
// (internal/orderclient); the service layer only depends on this interface so
// a delete can be unit-tested without it.
type ErrandCounter interface {
	ActiveErrands(ctx context.Context, supplierID string) (int, error)
}

// Service holds the business rules for supplier management (FR F2), on
// top of a storage-agnostic Repository.
type Service struct {
	repo Repository
	// AI-generated (edited by James Teo).
	errands ErrandCounter
}

// AI-generated (edited by James Teo).
// NewService returns a Service that stores suppliers in repo and asks
// errands before every delete. Both must be non-nil.
func NewService(repo Repository, errands ErrandCounter) *Service {
	return &Service{repo: repo, errands: errands}
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]*Supplier, error) {
	return s.repo.List(ctx, filter)
}

// parseID returns id in the canonical lower-case form Postgres stores, or
// ErrNotFound when it is not a UUID at all: a string that cannot be an id names
// no supplier, so it is answered like an unknown one and never reaches SQL or
// order-service. uuid.Parse also accepts forms Postgres does not (such as a
// "urn:uuid:" prefix), so callers must use the returned string, never the input.
func parseID(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", ErrNotFound
	}
	return parsed.String(), nil
}

func (s *Service) Get(ctx context.Context, id string) (*Supplier, error) {
	id, err := parseID(id)
	if err != nil {
		return nil, err
	}
	sup, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sup == nil {
		return nil, ErrNotFound
	}
	return sup, nil
}

// Create validates and inserts a new supplier (FR F2.2, F2.2.1, F2.2.2). It
// writes no audit row: it is for the first-boot seed, which is nobody's admin
// action. An admin's create goes through AdminCreate.
func (s *Service) Create(ctx context.Context, in *Supplier) (*Supplier, error) {
	return s.create(ctx, in, nil)
}

// AdminCreate is Create for an admin: the supplier and its audit row are saved
// together, and a refused or failed attempt is logged too (D-106, D-108, D-109).
func (s *Service) AdminCreate(ctx context.Context, ac AuditContext, in *Supplier) (*Supplier, error) {
	created, err := s.create(ctx, in, &ac)
	if err != nil {
		s.recordAttempt(ctx, ac, ActionCreate, "", attemptedFields(in), err)
		return nil, err
	}
	return created, nil
}

func (s *Service) create(ctx context.Context, in *Supplier, ac *AuditContext) (*Supplier, error) {
	if err := validate(in); err != nil {
		return nil, err
	}

	dup, err := s.repo.ExistsDuplicate(ctx, in.Name, in.Building, in.LocationDescription, "")
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, ErrDuplicate
	}

	now := time.Now().UTC()
	in.ID = uuid.NewString()
	in.CreatedAt = now
	in.UpdatedAt = now

	if ac == nil {
		if err := s.repo.Create(ctx, in); err != nil {
			return nil, err
		}
		return in, nil
	}
	e := ac.entry(ActionCreate, in.ID, OutcomeSucceeded, "")
	e.AdditionalComments = supplierFields(in)
	if err := s.repo.CreateAudited(ctx, in, e); err != nil {
		return nil, err
	}
	return in, nil
}

// attemptedFields is what an attempted create or update asked for, so a refused
// attempt's log row shows it. in may be a half-filled body.
func attemptedFields(in *Supplier) map[string]any {
	if in == nil {
		return nil
	}
	return supplierFields(in)
}

// Update validates and applies changes to an existing supplier (FR F2.2,
// F2.2.1, F2.2.2).
func (s *Service) Update(ctx context.Context, id string, in *Supplier) (*Supplier, error) {
	return s.update(ctx, id, in, nil)
}

// AdminUpdate is Update for an admin: the change and its audit row are saved
// together, and a refused or failed attempt is logged too (D-106, D-108, D-109).
// The row holds the new value of every field the edit changed (D-112).
func (s *Service) AdminUpdate(ctx context.Context, ac AuditContext, id string, in *Supplier) (*Supplier, error) {
	updated, err := s.update(ctx, id, in, &ac)
	if err != nil {
		s.recordAttempt(ctx, ac, ActionUpdate, auditTargetID(id), attemptedFields(in), err)
		return nil, err
	}
	return updated, nil
}

// auditTargetID is the id an attempt names, in canonical form when it is a
// UUID and as typed otherwise, so a refused attempt on a bad id is still found
// by what the admin sent.
func auditTargetID(id string) string {
	if canonical, err := parseID(id); err == nil {
		return canonical
	}
	return id
}

func (s *Service) update(ctx context.Context, id string, in *Supplier, ac *AuditContext) (*Supplier, error) {
	id, err := parseID(id)
	if err != nil {
		return nil, err
	}
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := validate(in); err != nil {
		return nil, err
	}

	dup, err := s.repo.ExistsDuplicate(ctx, in.Name, in.Building, in.LocationDescription, id)
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, ErrDuplicate
	}

	before := *existing
	existing.Name = in.Name
	existing.Type = in.Type
	existing.Building = in.Building
	existing.Floor = in.Floor
	existing.LocationDescription = in.LocationDescription
	existing.Latitude = in.Latitude
	existing.Longitude = in.Longitude
	existing.OpeningTime = in.OpeningTime
	existing.ClosingTime = in.ClosingTime
	existing.ImageURL = in.ImageURL
	existing.Description = in.Description
	existing.IsAvailable = in.IsAvailable
	existing.UpdatedAt = time.Now().UTC()

	if ac == nil {
		if err := s.repo.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	e := ac.entry(ActionUpdate, existing.ID, OutcomeSucceeded, "")
	e.AdditionalComments = changedFields(&before, existing)
	if err := s.repo.UpdateAudited(ctx, existing, e); err != nil {
		return nil, err
	}
	return existing, nil
}

// AI-generated (edited by James Teo).
// Delete soft-deletes a supplier, unless an errand that is not finished
// still references it, in which case it returns ErrInUse (FR F2.2.3).
//
// When the count cannot be had, Delete refuses with the counter's error:
// F2.2.3 forbids deleting a supplier an unfinished errand references, and
// without the count that cannot be ruled out.
//
// The delete itself stays soft (marks the record inactive and sets
// DeletedAt) instead of removing the row, so no data referenced by a
// historical order is ever lost.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.deleteSupplier(ctx, id, nil)
}

// AdminDelete is Delete for an admin: the delete and its audit row are saved
// together, and a refused or failed attempt is logged too (D-106, D-108, D-109).
func (s *Service) AdminDelete(ctx context.Context, ac AuditContext, id string) error {
	if err := s.deleteSupplier(ctx, id, &ac); err != nil {
		s.recordAttempt(ctx, ac, ActionDelete, auditTargetID(id), nil, err)
		return err
	}
	return nil
}

func (s *Service) deleteSupplier(ctx context.Context, id string, ac *AuditContext) error {
	id, err := parseID(id)
	if err != nil {
		return err
	}
	existing, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	n, err := s.errands.ActiveErrands(ctx, id)
	if err != nil {
		return fmt.Errorf("count unfinished errands for supplier %s: %w", id, err)
	}
	if n > 0 {
		return ErrInUse
	}
	if ac == nil {
		return s.repo.SoftDelete(ctx, id)
	}
	e := ac.entry(ActionDelete, id, OutcomeSucceeded, "")
	e.AdditionalComments = map[string]any{"name": existing.Name, "building": existing.Building}
	return s.repo.SoftDeleteAudited(ctx, id, e)
}

func validate(in *Supplier) error {
	var missing []string
	if strings.TrimSpace(in.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(in.Type) == "" {
		missing = append(missing, "type")
	}
	if strings.TrimSpace(in.Building) == "" {
		missing = append(missing, "building")
	}
	if strings.TrimSpace(in.LocationDescription) == "" {
		missing = append(missing, "location_description")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing required field(s): %s", ErrValidation, strings.Join(missing, ", "))
	}

	if in.Latitude < -90 || in.Latitude > 90 {
		return fmt.Errorf("%w: latitude out of range", ErrValidation)
	}
	if in.Longitude < -180 || in.Longitude > 180 {
		return fmt.Errorf("%w: longitude out of range", ErrValidation)
	}

	if in.OpeningTime != "" && !timePattern.MatchString(in.OpeningTime) {
		return fmt.Errorf("%w: opening_time must be HH:MM", ErrValidation)
	}
	if in.ClosingTime != "" && !timePattern.MatchString(in.ClosingTime) {
		return fmt.Errorf("%w: closing_time must be HH:MM", ErrValidation)
	}

	return nil
}
