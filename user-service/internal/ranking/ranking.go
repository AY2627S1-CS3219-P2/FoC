// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Keeping each courier's saved errand-ranking model (D-095, D-099):
//   an opaque JSON document order-service trains and reads back, each user
//   reaching only their own.
// Author review: PENDING — <reviewer to complete>

// Package ranking stores each user's errand-ranking model in a database of
// its own (D-095). The model is order-service's: user-service keeps it as an
// opaque JSON object, never reading what is inside, so it learns nothing
// about errands from it. order-service saves and loads it with the courier's
// own access token (D-099), so a user reaches only their own model.
package ranking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Errors the transport tells apart.
var (
	ErrNotFound  = errors.New("ranking model not found")
	ErrForbidden = errors.New("a user may reach only their own ranking model")
	ErrInvalid   = errors.New("invalid ranking model")
)

// Model is one user's saved model.
type Model struct {
	UserID uuid.UUID
	// Data is the model as order-service sent it: a JSON object.
	Data json.RawMessage
	// TrainedAt is when order-service trained it.
	TrainedAt time.Time
	// UpdatedAt is when it was last saved here.
	UpdatedAt time.Time
}

// Store keeps models, one per user, implemented by internal/repository.
type Store interface {
	// Get returns ErrNotFound when the user has no model.
	Get(ctx context.Context, userID uuid.UUID) (Model, error)
	// Put saves m, replacing the user's previous model.
	Put(ctx context.Context, m Model) error
}

// Service reads and saves models. It is safe for concurrent use.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a ready-to-use Service.
func NewService(store Store, now func() time.Time) *Service {
	return &Service{store: store, now: now}
}

// Get returns userID's model to callerID, who must be that user.
func (s *Service) Get(ctx context.Context, callerID, userID uuid.UUID) (Model, error) {
	if callerID != userID {
		return Model{}, ErrForbidden
	}
	m, err := s.store.Get(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Model{}, ErrNotFound
		}
		return Model{}, fmt.Errorf("load ranking model of %s: %w", userID, err)
	}
	return m, nil
}

// Put saves data as userID's model, for callerID, who must be that user.
func (s *Service) Put(ctx context.Context, callerID, userID uuid.UUID, data json.RawMessage, trainedAt time.Time) (Model, error) {
	if callerID != userID {
		return Model{}, ErrForbidden
	}
	if !isJSONObject(data) {
		return Model{}, fmt.Errorf("%w: model must be a JSON object", ErrInvalid)
	}
	if trainedAt.IsZero() {
		return Model{}, fmt.Errorf("%w: trained_at is required", ErrInvalid)
	}
	m := Model{UserID: userID, Data: data, TrainedAt: trainedAt.UTC(), UpdatedAt: s.now().UTC()}
	if err := s.store.Put(ctx, m); err != nil {
		return Model{}, fmt.Errorf("save ranking model of %s: %w", userID, err)
	}
	return m, nil
}

func isJSONObject(data json.RawMessage) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}
