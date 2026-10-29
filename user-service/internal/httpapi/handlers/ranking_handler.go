// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: GET and PUT /api/v1/users/{uid}/ranking-model, order-service's store
//   for each courier's errand-ranking model (D-095, D-099, D-100).
// Author review: PENDING — <reviewer to complete>

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"foc/user-service/internal/ranking"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// RankingModels reads and saves a user's ranking model; *ranking.Service
// satisfies it.
type RankingModels interface {
	Get(ctx context.Context, callerID, userID uuid.UUID) (ranking.Model, error)
	Put(ctx context.Context, callerID, userID uuid.UUID, data json.RawMessage, trainedAt time.Time) (ranking.Model, error)
}

// RankingDependencies contains only the operations required by RankingHandler.
type RankingDependencies struct {
	Models RankingModels
	Logger *slog.Logger
}

// RankingHandler serves the ranking-model endpoints.
type RankingHandler struct{ deps RankingDependencies }

// NewRankingHandler constructs a ranking handler with its required operations.
func NewRankingHandler(deps RankingDependencies) RankingHandler { return RankingHandler{deps: deps} }

// rankingModelBody is PUT's body. The model stays raw: user-service keeps it
// without reading it (D-095).
type rankingModelBody struct {
	Model     json.RawMessage `json:"model"`
	TrainedAt *time.Time      `json:"trained_at"`
}

type rankingModelResponse struct {
	UserID    uuid.UUID       `json:"user_id"`
	Model     json.RawMessage `json:"model"`
	TrainedAt time.Time       `json:"trained_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Get handles GET /api/v1/users/{uid}/ranking-model: the caller's own model.
func (h RankingHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, uid, ok := h.callerAndTarget(w, r)
	if !ok {
		return
	}
	m, err := h.deps.Models.Get(r.Context(), principal.UserID, uid)
	if err != nil {
		h.writeRankingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRankingResponse(m))
}

// Put handles PUT /api/v1/users/{uid}/ranking-model: saves the caller's own
// model, replacing the last one. The body is held to the recorded 10 KB limit;
// order-service keeps forests under it (D-100).
func (h RankingHandler) Put(w http.ResponseWriter, r *http.Request) {
	principal, uid, ok := h.callerAndTarget(w, r)
	if !ok {
		return
	}
	var body rankingModelBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.TrainedAt == nil || len(body.Model) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model and trained_at are required"})
		return
	}
	m, err := h.deps.Models.Put(r.Context(), principal.UserID, uid, body.Model, *body.TrainedAt)
	if err != nil {
		h.writeRankingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRankingResponse(m))
}

// callerAndTarget reads the verified caller and the path's uid; a malformed
// uid is 400, as the API rules require.
func (h RankingHandler) callerAndTarget(w http.ResponseWriter, r *http.Request) (Principal, uuid.UUID, bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return Principal{}, uuid.Nil, false
	}
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return Principal{}, uuid.Nil, false
	}
	if h.deps.Models == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("ranking models are required"), "ranking model unavailable")
		return Principal{}, uuid.Nil, false
	}
	return principal, uid, true
}

func (h RankingHandler) writeRankingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ranking.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "you can reach only your own ranking model"})
	case errors.Is(err, ranking.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ranking model not found"})
	case errors.Is(err, ranking.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model must be a JSON object"})
	default:
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "ranking model unavailable")
	}
}

func toRankingResponse(m ranking.Model) rankingModelResponse {
	return rankingModelResponse{UserID: m.UserID, Model: m.Data, TrainedAt: m.TrainedAt, UpdatedAt: m.UpdatedAt}
}
