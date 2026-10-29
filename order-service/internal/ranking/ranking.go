// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Training each courier's model when they open Find errands and
//   recommending one open errand from it (D-087, D-088, D-094, D-099, D-100).
// Author review: PENDING — <reviewer to complete>

// Package ranking trains a courier's random forest (internal/forest) on what
// they were shown and accepted, and recommends the open errand it scores
// highest, with the reason (D-094). Training happens while the courier opens
// Find errands, and the model is saved in user-service with their own token
// (D-099). It decides nothing about errands: a recommendation is advice the
// courier can ignore, and any failure here leaves the list as it was.
package ranking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/forest"
	"foc/order-service/internal/order"
)

// MinCompleted is how many errands a courier completes before they are shown
// a recommendation (D-094).
const MinCompleted = 2

// MaxExamples is the most shown errands one training uses, newest first.
const MaxExamples = 300

// minExamples is the fewest a model is trained on: fewer cannot fit a forest
// worth trusting.
const minExamples = 6

// SavedSizeBudget is the most bytes a saved model may take, leaving room under
// user-service's 10 KB request limit for the body around it (D-100).
const SavedSizeBudget = 9 * 1024

// RetrainAfter is how old a model may grow before new shown errands alone are
// reason to train again. A new accept or completion retrains at once.
const RetrainAfter = 10 * time.Minute

// savedVersion is written into every saved model.
const savedVersion = 1

// forestConfig is D-100's cap: at most 10 trees of depth 4.
func forestConfig() forest.Config { return forest.Config{Trees: 10, MaxDepth: 4, MinLeaf: 2} }

// ErrNoModel is what a ModelStore returns when the courier has none saved.
var ErrNoModel = errors.New("no saved ranking model")

// Example is one errand shown to the courier, and whether they then took it.
type Example struct {
	Candidate
	ShownAt  time.Time
	Accepted bool
}

// Source reads what training needs from order-service's own data;
// internal/repository implements it.
type Source interface {
	// ShownExamples returns the errands shown to the courier, once each at its
	// first showing, newest first, at most limit.
	ShownExamples(ctx context.Context, courierID uuid.UUID, limit int) ([]Example, error)
	// CourierHistory returns the courier's accepts, completions, ratings and
	// searches. RequesterRatings is left empty.
	CourierHistory(ctx context.Context, courierID uuid.UUID) (History, error)
	// RatingsReceived returns the ratings each of userIDs received.
	RatingsReceived(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]Rating, error)
}

// Saved is a model as user-service keeps it.
type Saved struct {
	Model     json.RawMessage
	TrainedAt time.Time
}

// ModelStore keeps one model per courier; internal/userclient implements it
// over user-service, forwarding the courier's own token from ctx (D-099).
type ModelStore interface {
	// Load returns ErrNoModel when the courier has none.
	Load(ctx context.Context, courierID uuid.UUID) (Saved, error)
	Save(ctx context.Context, courierID uuid.UUID, s Saved) error
}

// Recommendation is the errand the model suggests, how sure it is, and why.
type Recommendation struct {
	OrderID uuid.UUID
	// Confidence is the forest's score for the errand, from 0 to 1.
	Confidence float64
	// Reason is one or two sentences, the "1 to 3 lines" of D-094.
	Reason string
}

// Recommender trains and recommends. It is safe for concurrent use.
type Recommender struct {
	source Source
	models ModelStore
	clock  order.Clock
	newRNG func() *rand.Rand
	log    *slog.Logger
	places map[string]order.Location
}

// NewRecommender returns a ready-to-use Recommender. newRNG gives each
// training its random source; log records a model it could not save.
func NewRecommender(source Source, models ModelStore, clock order.Clock, newRNG func() *rand.Rand, log *slog.Logger) *Recommender {
	return &Recommender{source: source, models: models, clock: clock, newRNG: newRNG, log: log, places: placesByName()}
}

// model is a saved model's contents.
type model struct {
	Version  int            `json:"version"`
	Features []string       `json:"features"`
	Examples int            `json:"examples"`
	LastSeen time.Time      `json:"lastShown"`
	Forest   *forest.Forest `json:"forest"`
}

// Recommend returns the open errand courier c is most likely to take, or nil
// when there is none to suggest: too little history (D-094), no model yet, or
// nothing on the list they can accept. open is GET /open's list.
func (r *Recommender) Recommend(ctx context.Context, c order.Caller, open []order.Order) (*Recommendation, error) {
	if c.Role != order.RoleStudent {
		return nil, nil
	}
	var candidates []Candidate
	for _, o := range open {
		if o.RequesterID != c.UserID && !slices.Contains(o.WithdrawnCouriers, c.UserID) {
			candidates = append(candidates, CandidateOf(o))
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	h, err := r.source.CourierHistory(ctx, c.UserID)
	if err != nil {
		return nil, fmt.Errorf("load history of %s: %w", c.UserID, err)
	}
	if len(h.Completions) < MinCompleted {
		return nil, nil
	}
	m, err := r.current(ctx, c.UserID, h)
	if err != nil || m == nil {
		return nil, err
	}

	requesters := make([]uuid.UUID, 0, len(candidates))
	for _, cand := range candidates {
		requesters = append(requesters, cand.RequesterID)
	}
	if h.RequesterRatings, err = r.source.RatingsReceived(ctx, requesters); err != nil {
		return nil, fmt.Errorf("load requester ratings: %w", err)
	}
	now := r.clock.Now()
	var best *Recommendation
	for _, cand := range candidates {
		row := featuresAt(cand, now, h, r.places)
		e, err := m.Forest.Explain(row)
		if err != nil {
			return nil, fmt.Errorf("score errand %s: %w", cand.OrderID, err)
		}
		if best == nil || e.Score > best.Confidence {
			best = &Recommendation{OrderID: cand.OrderID, Confidence: e.Score, Reason: reasonFor(cand, row, e)}
		}
	}
	return best, nil
}

// current returns the courier's model, trained again first when it is
// missing or out of date, or nil when there is not yet enough to train on.
func (r *Recommender) current(ctx context.Context, courierID uuid.UUID, h History) (*model, error) {
	saved, err := r.models.Load(ctx, courierID)
	var m *model
	switch {
	case errors.Is(err, ErrNoModel):
	case err != nil:
		return nil, fmt.Errorf("load model of %s: %w", courierID, err)
	default:
		m = decodeModel(saved.Model)
	}

	examples, err := r.source.ShownExamples(ctx, courierID, MaxExamples)
	if err != nil {
		return nil, fmt.Errorf("load examples of %s: %w", courierID, err)
	}
	if m != nil && !r.stale(m, saved.TrainedAt, h, examples) {
		return m, nil
	}
	trained, err := r.train(ctx, examples, h)
	if err != nil || trained == nil {
		// Keep using the last model when training is not possible now.
		return m, err
	}
	data, err := json.Marshal(trained)
	if err != nil {
		return nil, fmt.Errorf("encode model: %w", err)
	}
	// A save that fails costs only the next training; this request still uses
	// the fresh model.
	if err := r.models.Save(ctx, courierID, Saved{Model: data, TrainedAt: r.clock.Now()}); err != nil {
		r.log.WarnContext(ctx, "ranking: could not save the courier's model", "courier_id", courierID, "err", err)
	}
	return trained, nil
}

// stale reports whether m no longer reflects what the courier has done: they
// accepted or completed something since, or it is old and they have seen
// more errands.
func (r *Recommender) stale(m *model, trainedAt time.Time, h History, examples []Example) bool {
	for _, a := range h.Accepts {
		if a.At.After(trainedAt) {
			return true
		}
	}
	for _, t := range h.Completions {
		if t.After(trainedAt) {
			return true
		}
	}
	seenMore := len(examples) > 0 && examples[0].ShownAt.After(m.LastSeen)
	return seenMore && r.clock.Now().Sub(trainedAt) > RetrainAfter
}

// train fits a forest to examples, newest first, halving them until the saved
// model fits SavedSizeBudget (D-100). It returns nil when there is too little
// to learn from: too few examples, or only one kind.
func (r *Recommender) train(ctx context.Context, examples []Example, h History) (*model, error) {
	if len(examples) < minExamples || !bothKinds(examples) {
		return nil, nil
	}
	requesters := make([]uuid.UUID, 0, len(examples))
	for _, e := range examples {
		requesters = append(requesters, e.RequesterID)
	}
	ratings, err := r.source.RatingsReceived(ctx, requesters)
	if err != nil {
		return nil, fmt.Errorf("load requester ratings: %w", err)
	}
	h.RequesterRatings = ratings

	for n := len(examples); n >= minExamples; n /= 2 {
		use := examples[:n]
		if !bothKinds(use) {
			return nil, nil
		}
		x := make([][]float64, len(use))
		y := make([]bool, len(use))
		for i, e := range use {
			x[i] = featuresAt(e.Candidate, e.ShownAt, h, r.places)
			y[i] = e.Accepted
		}
		f, err := forest.Train(x, y, forestConfig(), r.newRNG())
		if err != nil {
			return nil, fmt.Errorf("train forest: %w", err)
		}
		m := &model{Version: savedVersion, Features: FeatureNames(), Examples: n, LastSeen: use[0].ShownAt, Forest: f}
		data, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("encode model: %w", err)
		}
		if len(data) <= SavedSizeBudget {
			return m, nil
		}
	}
	return nil, nil
}

func bothKinds(examples []Example) bool {
	var yes, no bool
	for _, e := range examples {
		yes = yes || e.Accepted
		no = no || !e.Accepted
	}
	return yes && no
}

// decodeModel reads a saved model, or returns nil for one this build cannot
// use: unreadable, another version, or trained on other features.
func decodeModel(data json.RawMessage) *model {
	var m model
	if err := json.Unmarshal(data, &m); err != nil || m.Version != savedVersion || m.Forest == nil ||
		!slices.Equal(m.Features, FeatureNames()) || m.Forest.Features() != featureCount {
		return nil
	}
	return &m
}
