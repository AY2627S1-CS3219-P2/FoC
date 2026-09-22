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

// Service holds the business rules for supplier management (FR F2), on
// top of a storage-agnostic Repository.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]*Supplier, error) {
	return s.repo.List(ctx, filter)
}

func (s *Service) Get(ctx context.Context, id string) (*Supplier, error) {
	sup, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sup == nil {
		return nil, ErrNotFound
	}
	return sup, nil
}

// Create validates and inserts a new supplier (FR F2.2, F2.2.1, F2.2.2).
func (s *Service) Create(ctx context.Context, in *Supplier) (*Supplier, error) {
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
	if !in.IsAvailable {
		in.IsAvailable = true // default to available on creation
	}

	if err := s.repo.Create(ctx, in); err != nil {
		return nil, err
	}
	return in, nil
}

// Update validates and applies changes to an existing supplier (FR F2.2,
// F2.2.1, F2.2.2).
func (s *Service) Update(ctx context.Context, id string, in *Supplier) (*Supplier, error) {
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

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// Delete soft-deletes a supplier (FR F2.2.3).
//
// Interim design: hard-deletion should be blocked while a supplier is
// referenced by a non-terminal errand, but that check depends on the
// Order Service, which does not exist yet. Until that integration is
// wired up, Delete always soft-deletes (marks the record inactive and
// sets DeletedAt) instead of removing the row, so no data referenced by
// an in-flight or historical order is ever lost.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.repo.SoftDelete(ctx, id)
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
