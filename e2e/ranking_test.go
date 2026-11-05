//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-31
// Scope: The e2e scenario for errand ranking: a courier who always takes one
//   supplier type and passes over another is recommended the new errand of the
//   type they take (D-094, D-101), off a model order-service saves in
//   user-service with the courier's token (D-099, D-100), against the
//   services' recorded specs.
// Author review: nigeltzy — Checked that the boilerplate end-to-end tests were generated and according to the required test and expected behaviour.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// rankingRounds is how many errands of each type the courier is shown,
	// taking one of each pair: past D-094's 2 completions, and enough shown
	// errands to train on.
	rankingRounds = 4
	// minConfidence is the lowest confidence a recommendation is shown at (D-101).
	minConfidence = 0.4
	// maxModelBytes is D-100's limit on a saved model.
	maxModelBytes = 10 * 1024
	// likedType is the supplier type the courier always takes. It must not be
	// the type of errandInput's supplier, which the tests running alongside
	// post errands at: those would score as high as this test's own.
	likedType = "Printing"
)

// typedSupplier is supplier-service's list entry with its type.
type typedSupplier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	IsAvailable bool   `json:"is_available"`
}

// findFilters is order-service's FindFilters.
type findFilters struct {
	TimeLeft string `json:"timeLeft"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// anyFilters is Find errands with no filter set.
var anyFilters = findFilters{TimeLeft: "any"}

type shownItem struct {
	ErrandID string `json:"errandId"`
	Position int    `json:"position"`
}

// shownRequest is order-service's ShownRequest.
type shownRequest struct {
	Items   []shownItem `json:"items"`
	Filters findFilters `json:"filters"`
}

// recommendation is order-service's Recommendation.
type recommendation struct {
	ErrandID   string  `json:"errandId"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// openList is order-service's ErrandList as GET /open sends it.
type openList struct {
	Items          []errand        `json:"items"`
	Recommendation *recommendation `json:"recommendation"`
}

// rankingModel is user-service's RankingModelResponse.
type rankingModel struct {
	UserID string          `json:"user_id"`
	Model  json.RawMessage `json:"model"`
}

// suppliersWithTypes lists the suppliers taking errands, skipping the e2e-
// ones other tests create and delete (availableSupplier does the same).
func (s *stack) suppliersWithTypes(t *testing.T, u user) []typedSupplier {
	t.Helper()
	r := s.send(t, http.MethodGet, suppliersPath, u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var all []typedSupplier
	require.NoError(t, r.decode(&all))
	var out []typedSupplier
	for _, sup := range all {
		if sup.IsAvailable && !strings.HasPrefix(sup.Name, "e2e-") {
			out = append(out, sup)
		}
	}
	return out
}

// showErrands reports errands as having come into courier's view on Find
// errands, in list order, as the browser does.
func (s *stack) showErrands(t *testing.T, courier user, errands ...errand) {
	t.Helper()
	items := make([]shownItem, len(errands))
	for i, e := range errands {
		items[i] = shownItem{ErrandID: e.ID, Position: i}
	}
	r := s.send(t, http.MethodPost, ordersPath+"/activity/shown", courier.token, shownRequest{Items: items, Filters: anyFilters})
	expectStatus(t, r, http.StatusNoContent)
}

// openErrands is GET /open as u, with query appended to the path.
func (s *stack) openErrands(t *testing.T, u user, query string) (openList, response) {
	t.Helper()
	r := s.send(t, http.MethodGet, ordersPath+"/open"+query, u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var list openList
	require.NoError(t, r.decode(&list))
	return list, r
}

// cancelLeftover cancels errand id as its requester u when a test ends. It is
// best effort: an errand that has completed or been cancelled already refuses,
// which is fine. t.Context() has ended by then, so it uses its own.
func (s *stack) cancelLeftover(u user, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	_, _ = s.request(ctx, http.MethodPost, actionPath(id, "cancel"), u.token, nil, nil)
}

// TestCourierIsRecommendedTheTypeTheyTake shows a courier pairs of errands,
// one at a liked supplier type and one at the type everyone else posts at;
// they always take the liked one. Once they have completed enough, GET /open
// recommends the new liked errand, and the model behind it is theirs alone to
// read (D-094, D-099, D-100, D-101).
func TestCourierIsRecommendedTheTypeTheyTake(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)

	base := s.errandInput(t, requester, 1)
	var liked, passed typedSupplier
	for _, sup := range s.suppliersWithTypes(t, requester) {
		if sup.ID == base.SupplierID {
			passed = sup
		}
		if sup.Type == likedType && liked.ID == "" {
			liked = sup
		}
	}
	require.NotEmpty(t, liked.ID, "supplier-service lists no available %s supplier; it seeds itself from data/csv", likedType)
	require.NotEmpty(t, passed.ID)
	require.NotEqual(t, likedType, passed.Type,
		"errandInput's supplier %s is a %s supplier, so the other tests' errands would score like this test's; pick another likedType",
		passed.Name, passed.Type)

	post := func(sup typedSupplier, item string) errand {
		in := base
		in.SupplierID, in.ItemDescription = sup.ID, item
		e := s.createErrand(t, requester, in).Errand
		// An errand of the liked type left OPEN would be another run's
		// recommendation until T passes.
		t.Cleanup(func() { s.cancelLeftover(requester, e.ID) })
		return e
	}

	for round := range rankingRounds {
		take := post(liked, fmt.Sprintf("e2e liked %d", round))
		skip := post(passed, fmt.Sprintf("e2e passed %d", round))
		s.showErrands(t, courier, take, skip)
		done := s.complete(t, requester, courier, take)
		// Both rate, so neither is held by the rating gate (D-093).
		expectStatus(t, s.rate(t, requester, done.ID, 5), http.StatusCreated)
		expectStatus(t, s.rate(t, courier, done.ID, 5), http.StatusCreated)
		// Keeps the requester under the limit on errands OPEN at once.
		expectStatus(t, s.act(t, requester, skip.ID, "cancel"), http.StatusOK)
	}

	want := post(liked, "e2e liked new")
	post(passed, "e2e passed new")

	list, _ := s.openErrands(t, courier, "")
	require.NotNil(t, list.Recommendation, "a courier with %d completed errands got no recommendation", rankingRounds)
	rec := *list.Recommendation
	require.Equal(t, want.ID, rec.ErrandID, "recommended %s, want the new %s errand %s; reason: %s",
		rec.ErrandID, likedType, want.ID, rec.Reason)
	require.GreaterOrEqual(t, rec.Confidence, minConfidence)
	require.LessOrEqual(t, rec.Confidence, 1.0)
	require.NotEmpty(t, rec.Reason)

	// recommend=false sends the list without one.
	_, r := s.openErrands(t, courier, "?recommend=false")
	var raw map[string]json.RawMessage
	require.NoError(t, r.decode(&raw))
	require.NotContains(t, raw, "recommendation")

	// The model is saved in user-service under D-100's limit, and only its
	// courier may read it.
	modelPath := usersPath + "/" + courier.id + "/ranking-model"
	r = s.send(t, http.MethodGet, modelPath, courier.token, nil)
	expectStatus(t, r, http.StatusOK)
	var saved rankingModel
	require.NoError(t, r.decode(&saved))
	require.Equal(t, courier.id, saved.UserID)
	require.True(t, strings.HasPrefix(string(saved.Model), "{"), "the saved model is not a JSON object: %s", r)
	require.Less(t, len(saved.Model), maxModelBytes, "the saved model is %d bytes", len(saved.Model))
	expectStatus(t, s.send(t, http.MethodGet, modelPath, requester.token, nil), http.StatusForbidden)
}
