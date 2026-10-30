// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: HTTP tests for the ranking-model endpoints (D-095, D-099, D-100).
// Author review: PENDING — <reviewer to complete>

package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/ranking"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

// memoryRankingStore is a ranking.Store in a map, so these tests run the
// real ranking.Service behind the handler.
type memoryRankingStore struct {
	models map[uuid.UUID]ranking.Model
	err    error
}

func (m *memoryRankingStore) Get(_ context.Context, id uuid.UUID) (ranking.Model, error) {
	if m.err != nil {
		return ranking.Model{}, m.err
	}
	got, ok := m.models[id]
	if !ok {
		return ranking.Model{}, ranking.ErrNotFound
	}
	return got, nil
}

func (m *memoryRankingStore) Put(_ context.Context, model ranking.Model) error {
	if m.err != nil {
		return m.err
	}
	m.models[model.UserID] = model
	return nil
}

func rankingRouter(caller uuid.UUID, store *memoryRankingStore) http.Handler {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	return newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{principal: handlers.Principal{UserID: caller, Role: user.AccountRoleStudent}},
		Ranking:       handlers.RankingDependencies{Models: ranking.NewService(store, func() time.Time { return now })},
	})
}

func rankingRequest(method, uid, body string) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/users/"+uid+"/ranking-model", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer access-token")
	return request
}

func TestRankingModelRoundTrip(t *testing.T) {
	me := uuid.New()
	store := &memoryRankingStore{models: map[uuid.UUID]ranking.Model{}}
	router := rankingRouter(me, store)

	put := httptest.NewRecorder()
	router.ServeHTTP(put, rankingRequest(http.MethodPut, me.String(),
		`{"model":{"version":1,"trees":[]},"trained_at":"2026-10-29T11:59:00Z"}`))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d; body = %s", put.Code, put.Body.String())
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(get, rankingRequest(http.MethodGet, me.String(), ""))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d; body = %s", get.Code, get.Body.String())
	}
	var body struct {
		UserID    uuid.UUID       `json:"user_id"`
		Model     json.RawMessage `json:"model"`
		TrainedAt time.Time       `json:"trained_at"`
		UpdatedAt time.Time       `json:"updated_at"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.UserID != me || string(body.Model) != `{"version":1,"trees":[]}` ||
		!body.TrainedAt.Equal(time.Date(2026, 10, 29, 11, 59, 0, 0, time.UTC)) ||
		!body.UpdatedAt.Equal(time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("GET body = %s", get.Body.String())
	}
}

func TestRankingModelRefusals(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	valid := `{"model":{"v":1},"trained_at":"2026-10-29T11:59:00Z"}`
	tests := map[string]struct {
		method, uid, body string
		storeErr          error
		wantStatus        int
		wantError         string
	}{
		"reading someone else's model":  {http.MethodGet, other.String(), "", nil, http.StatusForbidden, "you can reach only your own ranking model"},
		"saving someone else's model":   {http.MethodPut, other.String(), valid, nil, http.StatusForbidden, "you can reach only your own ranking model"},
		"no model yet":                  {http.MethodGet, me.String(), "", nil, http.StatusNotFound, "ranking model not found"},
		"a malformed uid":               {http.MethodGet, "not-a-uuid", "", nil, http.StatusBadRequest, "invalid user id"},
		"no trained_at":                 {http.MethodPut, me.String(), `{"model":{"v":1}}`, nil, http.StatusBadRequest, "model and trained_at are required"},
		"no model":                      {http.MethodPut, me.String(), `{"trained_at":"2026-10-29T11:59:00Z"}`, nil, http.StatusBadRequest, "model and trained_at are required"},
		"a model that is not an object": {http.MethodPut, me.String(), `{"model":[1],"trained_at":"2026-10-29T11:59:00Z"}`, nil, http.StatusBadRequest, "model must be a JSON object"},
		"a body over 10 KB":             {http.MethodPut, me.String(), `{"model":{"pad":"` + strings.Repeat("x", 10*1024) + `"},"trained_at":"2026-10-29T11:59:00Z"}`, nil, http.StatusRequestEntityTooLarge, "request body too large"},
		"a store failure":               {http.MethodGet, me.String(), "", errors.New("database down"), http.StatusInternalServerError, "ranking model unavailable"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			store := &memoryRankingStore{models: map[uuid.UUID]ranking.Model{}, err: tt.storeErr}
			response := httptest.NewRecorder()
			rankingRouter(me, store).ServeHTTP(response, rankingRequest(tt.method, tt.uid, tt.body))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"error":"`+tt.wantError+`"`) {
				t.Fatalf("body = %s, want error %q", response.Body.String(), tt.wantError)
			}
		})
	}
}

func TestRankingModelNeedsAToken(t *testing.T) {
	router := newTestRouter(routes.Dependencies{
		TokenVerifier: &fakeTokenVerifier{err: errors.New("bad token")},
		Ranking:       handlers.RankingDependencies{Models: ranking.NewService(&memoryRankingStore{}, time.Now)},
	})
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, rankingRequest(method, uuid.NewString(), `{}`))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", method, response.Code)
		}
	}
}
