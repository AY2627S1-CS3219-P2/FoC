// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Handlers and bodies for ratings (D-093) and Find errands' shown and
//   search records (D-092).
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

// rateBody is POST /{errandId}/rating's body; a pointer tells a missing
// field from 0.
type rateBody struct {
	Stars *int `json:"stars"`
}

type filtersBody struct {
	TimeLeft *string `json:"timeLeft"`
	From     *string `json:"from"`
	To       *string `json:"to"`
}

type shownItemBody struct {
	ErrandID *uuid.UUID `json:"errandId"`
	Position *int       `json:"position"`
}

type shownBody struct {
	Items   []shownItemBody `json:"items"`
	Filters *filtersBody    `json:"filters"`
}

type searchBody struct {
	Filters *filtersBody `json:"filters"`
}

// RateErrand handles POST /{errandId}/rating (D-093).
func (s *server) RateErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	var body rateBody
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Stars == nil {
		writeError(w, http.StatusBadRequest, "stars is required")
		return
	}
	rating, err := s.svc.Rate(r.Context(), c, errandID, *body.Stars)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, gen.Rating{
		ErrandId:  rating.OrderID,
		RaterId:   rating.RaterID,
		RateeId:   rating.RateeID,
		RaterRole: gen.RaterRole(rating.RaterRole),
		Stars:     rating.Stars,
		RatedAt:   rating.RatedAt.UTC(),
	})
}

// ListPendingRatings handles GET /ratings/pending (D-093).
func (s *server) ListPendingRatings(w http.ResponseWriter, r *http.Request) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	pending, err := s.svc.PendingRatings(r.Context(), c)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.PendingRating, 0, len(pending))
	for _, p := range pending {
		items = append(items, gen.PendingRating{
			ErrandId:        p.OrderID,
			RateeId:         p.RateeID,
			RaterRole:       gen.RaterRole(p.RaterRole),
			ItemDescription: p.ItemDescription,
			CompletedAt:     p.CompletedAt.UTC(),
		})
	}
	writeJSON(w, http.StatusOK, gen.PendingRatingList{Items: items})
}

// GetUserRating handles GET /users/{userId}/rating (D-093).
func (s *server) GetUserRating(w http.ResponseWriter, r *http.Request, userID openapi_types.UUID) {
	if _, ok := callerOf(w, r); !ok {
		return
	}
	rating, err := s.svc.UserRating(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	out := gen.UserRating{UserId: rating.UserID, Count: rating.Count}
	if rating.Count > 0 {
		out.Average = ptr(rating.Average)
	}
	writeJSON(w, http.StatusOK, out)
}

// RecordShownErrands handles POST /activity/shown (D-092).
func (s *server) RecordShownErrands(w http.ResponseWriter, r *http.Request) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	var body shownBody
	if !decodeBody(w, r, &body) {
		return
	}
	f, msg := filtersOf(body.Filters)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	items := make([]order.Shown, 0, len(body.Items))
	for _, it := range body.Items {
		if it.ErrandID == nil || it.Position == nil {
			writeError(w, http.StatusBadRequest, "each item needs errandId and position")
			return
		}
		items = append(items, order.Shown{OrderID: *it.ErrandID, Position: *it.Position})
	}
	if err := s.activity.RecordShown(r.Context(), c, items, f); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RecordSearch handles POST /activity/searches (D-092).
func (s *server) RecordSearch(w http.ResponseWriter, r *http.Request) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	var body searchBody
	if !decodeBody(w, r, &body) {
		return
	}
	f, msg := filtersOf(body.Filters)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.activity.RecordSearch(r.Context(), c, f); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// filtersOf maps the filters body, or returns why it cannot: every field is
// required, an empty from or to meaning "any".
func filtersOf(b *filtersBody) (order.Filters, string) {
	if b == nil || b.TimeLeft == nil || b.From == nil || b.To == nil {
		return order.Filters{}, "filters with timeLeft, from and to is required"
	}
	return order.Filters{TimeLeft: *b.TimeLeft, From: *b.From, To: *b.To}, ""
}
