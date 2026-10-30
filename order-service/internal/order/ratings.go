// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Ratings (D-093): each side of a COMPLETED errand rates the other
//   once, 1 to 5 stars; a student with an errand left to rate can neither
//   post nor accept; anyone signed in can read a user's rating.
// Author review: PENDING — <reviewer to complete>

package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The range of stars a rating may give (D-091, D-093).
const (
	MinStars = 1
	MaxStars = 5
)

// PendingRatingLimit caps the list of errands a student has still to rate.
const PendingRatingLimit = 50

// RaterRole is which side of the errand the rater was.
type RaterRole string

// The two sides. A requester rates the courier; a courier rates the requester.
const (
	RaterRequester RaterRole = "requester"
	RaterCourier   RaterRole = "courier"
)

// Rating is one side's rating of the other on one errand. It cannot change.
type Rating struct {
	OrderID   uuid.UUID
	RaterID   uuid.UUID
	RateeID   uuid.UUID
	RaterRole RaterRole
	Stars     int
	RatedAt   time.Time
}

// PendingRating is a COMPLETED errand the student has not rated yet.
type PendingRating struct {
	OrderID         uuid.UUID
	RateeID         uuid.UUID
	RaterRole       RaterRole
	ItemDescription string
	CompletedAt     time.Time
}

// UserRating is what a user has received, as requester and courier together.
// Average is meaningful only when Count is above 0.
type UserRating struct {
	UserID  uuid.UUID
	Count   int
	Average float64
}

// ErrAlreadyRated: the rater has already rated that errand. Returned by
// RatingStore.SaveRating.
var ErrAlreadyRated = errors.New("errand already rated by this user")

// RatingStore keeps ratings (D-089, D-093), implemented by internal/repository
// and by fakes in tests.
type RatingStore interface {
	// SaveRating stores r, or returns ErrAlreadyRated when r.RaterID already
	// rated r.OrderID.
	SaveRating(ctx context.Context, r Rating) error
	// PendingRatings returns the user's COMPLETED errands, as requester or as
	// courier, that they have not rated, most recently completed first.
	PendingRatings(ctx context.Context, userID uuid.UUID, limit int) ([]PendingRating, error)
	// HasPendingRating reports whether PendingRatings would return any.
	HasPendingRating(ctx context.Context, userID uuid.UUID) (bool, error)
	// UserRating returns the ratings the user has received.
	UserRating(ctx context.Context, userID uuid.UUID) (UserRating, error)
}

// Rate saves c's rating of the other side of a COMPLETED errand (D-093).
func (s *Service) Rate(ctx context.Context, c Caller, id uuid.UUID, stars int) (Rating, error) {
	if c.Role != RoleStudent {
		return Rating{}, forbidden("only students rate errands")
	}
	if stars < MinStars || stars > MaxStars {
		return Rating{}, invalid(fmt.Sprintf("stars must be a whole number from %d to %d", MinStars, MaxStars))
	}
	o, err := s.load(ctx, id)
	if err != nil {
		return Rating{}, err
	}
	if !CanView(o, c, s.clock.Now()) {
		return Rating{}, notFound()
	}
	r := Rating{OrderID: o.ID, RaterID: c.UserID, Stars: stars, RatedAt: s.clock.Now()}
	switch RelationshipOf(o, c) {
	case RelRequester:
		r.RaterRole = RaterRequester
		if o.CourierID != nil {
			r.RateeID = *o.CourierID
		}
	case RelCourier:
		r.RaterRole = RaterCourier
		r.RateeID = o.RequesterID
	default:
		return Rating{}, forbidden("only the errand's requester and courier can rate it")
	}
	if o.State != StateCompleted || r.RateeID == uuid.Nil {
		return Rating{}, conflict("an errand can be rated once it is completed")
	}
	switch err := s.ratings.SaveRating(ctx, r); {
	case errors.Is(err, ErrAlreadyRated):
		return Rating{}, conflict("you have already rated this errand")
	case err != nil:
		return Rating{}, fmt.Errorf("save rating of %s by %s: %w", id, c.UserID, err)
	}
	return r, nil
}

// PendingRatings lists c's COMPLETED errands still to rate (D-093). An ADMIN
// neither posts nor carries errands, so has none.
func (s *Service) PendingRatings(ctx context.Context, c Caller) ([]PendingRating, error) {
	if c.Role != RoleStudent {
		return nil, nil
	}
	p, err := s.ratings.PendingRatings(ctx, c.UserID, PendingRatingLimit)
	if err != nil {
		return nil, fmt.Errorf("list pending ratings of %s: %w", c.UserID, err)
	}
	return p, nil
}

// UserRating returns the ratings userID has received. Any signed-in caller
// may read it: ratings are public (D-093).
func (s *Service) UserRating(ctx context.Context, userID uuid.UUID) (UserRating, error) {
	r, err := s.ratings.UserRating(ctx, userID)
	if err != nil {
		return UserRating{}, fmt.Errorf("load rating of %s: %w", userID, err)
	}
	return r, nil
}

// requireRated refuses c when they have a COMPLETED errand still to rate: a
// rating is required before posting or accepting another (D-093).
func (s *Service) requireRated(ctx context.Context, c Caller) error {
	pending, err := s.ratings.HasPendingRating(ctx, c.UserID)
	if err != nil {
		return fmt.Errorf("check pending ratings of %s: %w", c.UserID, err)
	}
	if pending {
		return conflict("rate your completed errand before posting or accepting another")
	}
	return nil
}
