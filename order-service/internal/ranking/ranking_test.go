// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Unit tests for the features, training, saving and recommending
//   (D-088, D-094, D-096, D-099, D-100).
// Author review: PENDING — <reviewer to complete>

package ranking

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/forest"
	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
	"foc/order-service/internal/shuttle"
)

// Monday 2026-10-26 10:00 in Singapore.
var monday10 = time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC)

var (
	com3Point  = &shuttle.Point{Lat: 1.294920, Lon: 103.775011}
	utownPoint = &shuttle.Point{Lat: 1.303535, Lon: 103.774431}
)

func candidate(supplierType string, pickup *shuttle.Point, drop string) Candidate {
	return Candidate{
		OrderID: uuid.New(), RequesterID: uuid.New(), Reward: 5, CreatedAt: monday10.Add(-20 * time.Minute),
		SupplierID: uuid.New(), SupplierName: "Supplier " + supplierType, SupplierType: supplierType,
		Pickup: pickup, DeliveryLocation: drop,
	}
}

func TestFeaturesUseOnlyWhatCameBefore(t *testing.T) {
	c := candidate("Food", com3Point, "COM1 Lobby")
	before, after := monday10.Add(-time.Hour), monday10.Add(time.Hour)
	h := History{
		Accepts: []Accept{
			{At: before, SupplierType: "Food", Until: &before},                          // Monday 09:00, Food
			{At: before.Add(-24 * time.Hour), SupplierType: "Printing", Until: &before}, // Sunday 09:00
			{At: after, SupplierType: "Food"},                                           // later: ignored
		},
		Completions: []time.Time{before, after},
		Ratings: []Rating{
			{Stars: 4, At: before, SupplierType: "Food"},
			{Stars: 2, At: before, SupplierType: "Printing"},
			{Stars: 1, At: after, SupplierType: "Food"},
		},
		Searches: []Search{
			{From: c.SupplierID.String(), To: "area:Business & Computing", At: before},
			{From: "", To: "place:RC4 lobby", At: before},
			{From: c.SupplierID.String(), At: after},
		},
		RequesterRatings: map[uuid.UUID][]Rating{c.RequesterID: {{Stars: 5, At: before}, {Stars: 3, At: before}}},
	}

	row := featuresAt(c, monday10, h, placesByName())

	assert.Equal(t, 5.0, row[fReward])
	assert.InDelta(t, 20, row[fAgeMinutes], 1e-9)
	assert.InDelta(t, 100, row[fTripMetres], 60, "COM3 stop to COM1 Lobby")
	assert.Equal(t, 10.0, row[fHour], "Singapore time")
	assert.Equal(t, float64(time.Monday), row[fWeekday])
	assert.Equal(t, 1.0, row[fHourShare], "both earlier accepts were at 09:00, within an hour of 10:00")
	assert.Equal(t, 0.5, row[fWeekdayShare])
	assert.Equal(t, 1.0, row[fCompleted])
	assert.Equal(t, 0.5, row[fTypeShare])
	assert.Equal(t, 4.0, row[fTypeRating])
	assert.Equal(t, 4.0, row[fRequesterRating])
	assert.Equal(t, 0.5, row[fSearchedPickup])
	assert.Equal(t, 0.5, row[fSearchedDestination], "the area filter takes in COM1 Lobby; the RC4 one does not")
	assert.Equal(t, 0.0, row[fCarrying])
	assert.Equal(t, float64(missing), row[fDetourMetres])
	assert.Equal(t, float64(missing), row[fSharesRoute])
}

func TestFeaturesOfACourierWithNoHistory(t *testing.T) {
	row := featuresAt(candidate("", nil, "Somewhere unlisted"), monday10, History{}, placesByName())
	for _, f := range []int{fTripMetres, fHourShare, fWeekdayShare, fTypeShare, fTypeRating, fRequesterRating,
		fDetourMetres, fSharesRoute, fSearchedPickup, fSearchedDestination} {
		assert.Equal(t, float64(missing), row[f], FeatureNames()[f])
	}
	assert.Zero(t, row[fCompleted])
	assert.Zero(t, row[fCarrying])
}

func TestTheErrandBeingCarriedShapesTheDetourFeatures(t *testing.T) {
	ended := monday10.Add(-10 * time.Minute)
	h := History{Accepts: []Accept{
		// Carried now: from COM3 to Fine Food at UTown.
		{At: monday10.Add(-5 * time.Minute), Pickup: com3Point, DeliveryLocation: "Fine Food, Town Plaza"},
		// Delivered earlier: no longer carried.
		{At: monday10.Add(-time.Hour), Until: &ended, Pickup: utownPoint, DeliveryLocation: "PGP Aircon Canteen"},
	}}

	onTheWay := featuresAt(candidate("Food", utownPoint, "RC4 lobby"), monday10, h, placesByName())
	assert.Equal(t, 1.0, onTheWay[fCarrying])
	assert.Less(t, onTheWay[fDetourMetres], 100.0, "the UTown pickup is beside the carried errand's drop-off")
	assert.Equal(t, 1.0, onTheWay[fSharesRoute])

	// Bukit Timah campus is far, but route P links Oei Tiong Ham to UTown.
	bukitTimah := featuresAt(candidate("Food", &shuttle.Point{Lat: 1.3196, Lon: 103.8178}, "RC4 lobby"), monday10, h, placesByName())
	assert.Greater(t, bukitTimah[fDetourMetres], 2000.0)
	assert.Equal(t, 1.0, bukitTimah[fSharesRoute])

	// Tampines has no NUS stop at all.
	offNetwork := featuresAt(candidate("Food", &shuttle.Point{Lat: 1.35, Lon: 103.95}, "RC4 lobby"), monday10, h, placesByName())
	assert.Equal(t, 0.0, offNetwork[fSharesRoute])
}

func TestHourGapWrapsMidnight(t *testing.T) {
	assert.Equal(t, 1, hourGap(23, 0))
	assert.Equal(t, 1, hourGap(0, 23))
	assert.Equal(t, 12, hourGap(6, 18))
	assert.Equal(t, 0, hourGap(9, 9))
}

// fakeSource is a Source over fixed data.
type fakeSource struct {
	examples []Example
	history  History
	ratings  map[uuid.UUID][]Rating
	err      error
}

func (f *fakeSource) ShownExamples(_ context.Context, _ uuid.UUID, limit int) ([]Example, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.examples[:min(limit, len(f.examples))], nil
}

func (f *fakeSource) CourierHistory(context.Context, uuid.UUID) (History, error) {
	return f.history, f.err
}

func (f *fakeSource) RatingsReceived(context.Context, []uuid.UUID) (map[uuid.UUID][]Rating, error) {
	return f.ratings, f.err
}

// fakeModels keeps saved models in a map.
type fakeModels struct {
	saved   map[uuid.UUID]Saved
	saves   int
	loadErr error
	saveErr error
}

func (f *fakeModels) Load(_ context.Context, id uuid.UUID) (Saved, error) {
	if f.loadErr != nil {
		return Saved{}, f.loadErr
	}
	s, ok := f.saved[id]
	if !ok {
		return Saved{}, ErrNoModel
	}
	return s, nil
}

func (f *fakeModels) Save(_ context.Context, id uuid.UUID, s Saved) error {
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved[id] = s
	return nil
}

// foodLover is a courier who took every Food errand shown and passed over every
// Printing one, with two completions behind them.
func foodLover(n int) *fakeSource {
	src := &fakeSource{history: History{Completions: []time.Time{monday10.Add(-48 * time.Hour), monday10.Add(-47 * time.Hour)}}}
	for i := range n {
		shown := monday10.Add(-time.Duration(i+1) * time.Hour)
		kind, took := "Printing", false
		if i%2 == 0 {
			kind, took = "Food", true
			src.history.Accepts = append(src.history.Accepts, Accept{At: shown.Add(time.Minute), SupplierType: kind, Until: ptr(shown.Add(30 * time.Minute))})
		}
		c := candidate(kind, com3Point, "COM1 Lobby")
		c.CreatedAt = shown.Add(-10 * time.Minute)
		src.examples = append(src.examples, Example{Candidate: c, ShownAt: shown, Accepted: took})
	}
	return src
}

func ptr[T any](v T) *T { return &v }

func openErrand(supplierType string) order.Order {
	return order.Order{
		ID: uuid.New(), RequesterID: uuid.New(), Reward: 5, State: order.StateOpen, CreatedAt: monday10.Add(-15 * time.Minute),
		Supplier:         order.SupplierSnapshot{ID: uuid.New(), Name: supplierType + " place", Type: supplierType, Point: com3Point},
		DeliveryLocation: "COM1 Lobby",
	}
}

func newRecommender(src Source, models ModelStore) *Recommender {
	return NewRecommender(src, models, ordertest.NewClock(monday10), func() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) },
		slog.New(slog.DiscardHandler))
}

var courier = order.Caller{UserID: uuid.New(), Role: order.RoleStudent}

func TestItRecommendsTheKindOfErrandTheCourierTakes(t *testing.T) {
	models := &fakeModels{saved: map[uuid.UUID]Saved{}}
	r := newRecommender(foodLover(20), models)
	food, printing := openErrand("Food"), openErrand("Printing")

	rec, err := r.Recommend(context.Background(), courier, []order.Order{printing, food})

	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, food.ID, rec.OrderID)
	assert.GreaterOrEqual(t, rec.Confidence, MinConfidence)
	assert.LessOrEqual(t, rec.Confidence, 1.0)
	assert.True(t, strings.HasPrefix(rec.Reason, "Suggested"), rec.Reason)
	assert.LessOrEqual(t, len(rec.Reason), 200, "1 to 3 lines")

	require.Equal(t, 1, models.saves, "the new model is saved")
	saved := models.saved[courier.UserID]
	assert.LessOrEqual(t, len(saved.Model), SavedSizeBudget, "D-100's budget")
	assert.Equal(t, monday10, saved.TrainedAt)
}

func TestASavedModelIsReusedUntilSomethingChanges(t *testing.T) {
	models := &fakeModels{saved: map[uuid.UUID]Saved{}}
	src := foodLover(20)
	r := newRecommender(src, models)
	open := []order.Order{openErrand("Food")}

	_, err := r.Recommend(context.Background(), courier, open)
	require.NoError(t, err)
	_, err = r.Recommend(context.Background(), courier, open)
	require.NoError(t, err)
	assert.Equal(t, 1, models.saves, "nothing changed, so no second training")

	src.history.Accepts = append(src.history.Accepts, Accept{At: monday10.Add(time.Second), SupplierType: "Food"})
	saved := models.saved[courier.UserID]
	saved.TrainedAt = monday10
	models.saved[courier.UserID] = saved
	_, err = r.Recommend(context.Background(), courier, open)
	require.NoError(t, err)
	assert.Equal(t, 2, models.saves, "a new accept retrains at once")
}

func TestNoRecommendation(t *testing.T) {
	onlyTook := foodLover(10)
	for i := range onlyTook.examples {
		onlyTook.examples[i].Accepted = true
	}
	cases := map[string]struct {
		caller order.Caller
		src    *fakeSource
		open   []order.Order
	}{
		"for an ADMIN": {order.Caller{UserID: uuid.New(), Role: order.RoleAdmin}, foodLover(20), []order.Order{openErrand("Food")}},
		"before two completed errands (D-094)": {courier, func() *fakeSource {
			s := foodLover(20)
			s.history.Completions = s.history.Completions[:1]
			return s
		}(), []order.Order{openErrand("Food")}},
		"with too few shown errands to learn from": {courier, foodLover(3), []order.Order{openErrand("Food")}},
		"when the courier took everything shown":   {courier, onlyTook, []order.Order{openErrand("Food")}},
		"when the list is empty":                   {courier, foodLover(20), nil},
		// AI-generated (edited by PENDING): D-101.
		"when the best errand scores under 40% (D-101)": {courier, foodLover(20), []order.Order{openErrand("Printing")}},
		"when every errand is the courier's own or withdrawn from": {courier, foodLover(20), func() []order.Order {
			own, withdrawn := openErrand("Food"), openErrand("Food")
			own.RequesterID = courier.UserID
			withdrawn.WithdrawnCouriers = []uuid.UUID{courier.UserID}
			return []order.Order{own, withdrawn}
		}()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec, err := newRecommender(tc.src, &fakeModels{saved: map[uuid.UUID]Saved{}}).Recommend(context.Background(), tc.caller, tc.open)
			require.NoError(t, err)
			assert.Nil(t, rec)
		})
	}
}

func TestFailures(t *testing.T) {
	t.Run("a source failure is an error", func(t *testing.T) {
		src := foodLover(20)
		src.err = errors.New("database down")
		_, err := newRecommender(src, &fakeModels{saved: map[uuid.UUID]Saved{}}).Recommend(context.Background(), courier, []order.Order{openErrand("Food")})
		require.ErrorIs(t, err, src.err)
	})
	t.Run("a model that cannot be loaded is an error", func(t *testing.T) {
		models := &fakeModels{saved: map[uuid.UUID]Saved{}, loadErr: errors.New("user-service down")}
		_, err := newRecommender(foodLover(20), models).Recommend(context.Background(), courier, []order.Order{openErrand("Food")})
		require.ErrorIs(t, err, models.loadErr)
	})
	t.Run("a model that cannot be saved still recommends", func(t *testing.T) {
		models := &fakeModels{saved: map[uuid.UUID]Saved{}, saveErr: errors.New("user-service down")}
		rec, err := newRecommender(foodLover(20), models).Recommend(context.Background(), courier, []order.Order{openErrand("Food")})
		require.NoError(t, err)
		assert.NotNil(t, rec)
	})
	t.Run("a saved model this build cannot read is trained again", func(t *testing.T) {
		models := &fakeModels{saved: map[uuid.UUID]Saved{courier.UserID: {Model: json.RawMessage(`{"version":99}`), TrainedAt: monday10}}}
		rec, err := newRecommender(foodLover(20), models).Recommend(context.Background(), courier, []order.Order{openErrand("Food")})
		require.NoError(t, err)
		assert.NotNil(t, rec)
		assert.Equal(t, 1, models.saves)
	})
}

func TestReasons(t *testing.T) {
	c := candidate("Food", com3Point, "COM1 Lobby")
	row := featuresAt(c, monday10, History{}, placesByName())
	row[fTypeShare], row[fHourShare] = 0.8, 0.5

	reason := reasonFor(c, row, explanation(map[int]float64{fTypeShare: 0.3, fHour: 0.2, fHourShare: 0.1, fReward: -0.4}))
	assert.Equal(t, "Suggested because you often take Food errands and you often take errands around this time.", reason)

	assert.Equal(t, fallbackReason, reasonFor(c, row, explanation(map[int]float64{fReward: -0.1})))
	assert.Equal(t, "350 m", metresText(347))
	assert.Equal(t, "1.2 km", metresText(1234))
	assert.Equal(t, "2 h 10 min", minutesText(130))
	assert.Equal(t, "45 min", minutesText(45))
}

func explanation(by map[int]float64) (e forest.Explanation) {
	e.Contributions = make([]float64, featureCount)
	for f, v := range by {
		e.Contributions[f] = v
	}
	return e
}
