// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The handlers behind every route in api/openapi.yaml (D-053): decode,
//   call order.Service, map its error kinds to status codes (D-046).
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

const (
	// healthTimeout bounds GET /health's database ping.
	healthTimeout = 2 * time.Second
	// maxRequestKeyLength is api/openapi.yaml's maxLength for X-Request-Id.
	maxRequestKeyLength = 64
)

// server implements gen.ServerInterface on top of order.Service. Handlers do
// transport only (root AGENTS.md §6).
type server struct {
	svc *order.Service
	db  Pinger
	log *slog.Logger
}

// GetHealth handles GET /health: 200 while the database answers, else 503.
func (s *server) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		s.log.WarnContext(r.Context(), "health check: database did not answer",
			"request_id", order.CorrelationID(r.Context()), "err", err)
		writeJSON(w, http.StatusServiceUnavailable, gen.Health{Status: gen.Unhealthy})
		return
	}
	writeJSON(w, http.StatusOK, gen.Health{Status: gen.Ok})
}

// CreateErrand handles POST / (F3.1). The client's X-Request-Id is the
// duplicate-submission key: a key already used by this requester answers 200
// with the errand it created (D-047). A key must pass validRequestID, the rule
// that keeps it as the correlation id, so the two are always the same id.
func (s *server) CreateErrand(w http.ResponseWriter, r *http.Request, params gen.CreateErrandParams) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	var key string
	if params.XRequestId != nil {
		key = *params.XRequestId
	}
	if utf8.RuneCountInString(key) > maxRequestKeyLength {
		writeError(w, http.StatusBadRequest, "X-Request-Id can be at most 64 characters")
		return
	}
	if key != "" && !validRequestID(key) {
		writeError(w, http.StatusBadRequest, "X-Request-Id must be valid UTF-8 text of at most 255 bytes")
		return
	}
	var body gen.CreateErrandRequest
	if !decodeBody(w, r, &body) {
		return
	}
	o, replayed, err := s.svc.Create(r.Context(), c, createInput(body, key, order.CorrelationID(r.Context())))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	s.writeDetailAfter(w, r, c, o, status)
}

// ListActiveErrands handles GET /?scope=active: every errand that is not
// final, for an ADMIN (D-053).
func (s *server) ListActiveErrands(w http.ResponseWriter, r *http.Request, params gen.ListActiveErrandsParams) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	if !params.Scope.Valid() {
		writeError(w, http.StatusBadRequest, "scope must be active")
		return
	}
	orders, err := s.svc.ListActive(r.Context(), c)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	s.writeList(w, c, orders)
}

// ListOpenErrands handles GET /open: the errands couriers can accept (F3.2).
func (s *server) ListOpenErrands(w http.ResponseWriter, r *http.Request) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	orders, err := s.svc.ListOpen(r.Context(), c)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	s.writeList(w, c, orders)
}

// ListRequestedErrands handles GET /requested: the caller's own errands (F3.4).
func (s *server) ListRequestedErrands(w http.ResponseWriter, r *http.Request, params gen.ListRequestedErrandsParams) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	if !params.Scope.Valid() {
		writeError(w, http.StatusBadRequest, "scope must be active or history")
		return
	}
	orders, err := s.svc.ListRequested(r.Context(), c, order.Scope(params.Scope))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	s.writeList(w, c, orders)
}

// ListAssignedErrands handles GET /assigned: the caller's assignments as
// courier, current ones in items or ended ones in history (F3.5).
func (s *server) ListAssignedErrands(w http.ResponseWriter, r *http.Request, params gen.ListAssignedErrandsParams) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	switch params.Scope {
	case gen.ListAssignedErrandsParamsScopeActive:
		orders, err := s.svc.ListAssigned(r.Context(), c)
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}
		now := s.svc.Now()
		writeJSON(w, http.StatusOK, gen.AssignedList{
			Items:      toErrands(orders, c, now),
			History:    []gen.AssignmentRecord{},
			ServerTime: now.UTC(),
		})
	case gen.ListAssignedErrandsParamsScopeHistory:
		records, err := s.svc.ListAssignmentHistory(r.Context(), c)
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, gen.AssignedList{
			Items:      []gen.Errand{},
			History:    toAssignments(records),
			ServerTime: s.svc.Now().UTC(),
		})
	default:
		writeError(w, http.StatusBadRequest, "scope must be active or history")
	}
}

// ListDeliveryLocations handles GET /locations: the campus list (D-053).
func (s *server) ListDeliveryLocations(w http.ResponseWriter, r *http.Request) {
	if _, ok := callerOf(w, r); !ok {
		return
	}
	locations := order.DeliveryLocations()
	items := make([]gen.CampusLocation, len(locations))
	for i, l := range locations {
		items[i] = gen.CampusLocation{Name: l.Name, Area: l.Area}
	}
	writeJSON(w, http.StatusOK, gen.LocationList{Items: items})
}

// CountSupplierActiveErrands handles GET /suppliers/{supplierId}/active-errands,
// supplier-service's check before it deletes a supplier, made with the
// admin's token (D-052).
func (s *server) CountSupplierActiveErrands(w http.ResponseWriter, r *http.Request, supplierID uuid.UUID) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	n, err := s.svc.CountSupplierActive(r.Context(), c, supplierID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.SupplierActiveErrands{SupplierId: supplierID, ActiveErrands: n})
}

// GetErrand handles GET /{errandId}: the errand and its history, or 404 when
// the caller may not see it (F3.6).
func (s *server) GetErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	o, history, err := s.svc.Get(r.Context(), c, errandID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDetail(o, history, c, s.svc.Now()))
}

// AmendErrand handles PATCH /{errandId}: the requester changes an OPEN errand
// (F3.7); a field left out is unchanged.
func (s *server) AmendErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	var body amendBody
	if !decodeBody(w, r, &body) {
		return
	}
	in, ok := body.input(order.CorrelationID(r.Context()))
	if !ok {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	o, err := s.svc.Amend(r.Context(), c, errandID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	s.writeDetailAfter(w, r, c, o, http.StatusOK)
}

// AcceptErrand handles POST /{errandId}/accept with the version the courier
// was shown (F3.3, D-046).
func (s *server) AcceptErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	var body versionBody
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Version == nil {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	o, err := s.svc.Accept(r.Context(), c, errandID, *body.Version, order.CorrelationID(r.Context()))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	s.writeDetailAfter(w, r, c, o, http.StatusOK)
}

// WithdrawErrand handles POST /{errandId}/withdraw (F3.12).
func (s *server) WithdrawErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	s.runAction(w, r, errandID, s.svc.Withdraw)
}

// PickUpErrand handles POST /{errandId}/pickup (F3.8).
func (s *server) PickUpErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	s.runAction(w, r, errandID, s.svc.PickUp)
}

// DeliverErrand handles POST /{errandId}/deliver (F3.9).
func (s *server) DeliverErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	s.runAction(w, r, errandID, s.svc.Deliver)
}

// ConfirmErrand handles POST /{errandId}/confirm (F3.9.2, F3.9.3).
func (s *server) ConfirmErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	s.runAction(w, r, errandID, s.svc.Confirm)
}

// CancelErrand handles POST /{errandId}/cancel: the requester's cancel or an
// ADMIN's, as the service decides from the caller (F3.11, D-045).
func (s *server) CancelErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	s.runAction(w, r, errandID, s.svc.Cancel)
}

// CompleteErrand handles POST /{errandId}/complete, an ADMIN's (D-045).
func (s *server) CompleteErrand(w http.ResponseWriter, r *http.Request, errandID uuid.UUID) {
	s.runAction(w, r, errandID, s.svc.Complete)
}

// action is the shape every body-less lifecycle action of order.Service shares.
type action func(ctx context.Context, c order.Caller, id uuid.UUID, correlationID string) (order.Order, error)

// runAction runs a body-less action for the caller and answers with the errand
// after it.
func (s *server) runAction(w http.ResponseWriter, r *http.Request, errandID uuid.UUID, act action) {
	c, ok := callerOf(w, r)
	if !ok {
		return
	}
	o, err := act(r.Context(), c, errandID, order.CorrelationID(r.Context()))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	s.writeDetailAfter(w, r, c, o, http.StatusOK)
}

// writeDetailAfter answers an action that has committed with the errand and
// its history as c now sees them. The action's own result stands in when that
// re-read fails: answering an error would make the client retry an action that
// already happened. A caller who can no longer see the errand, because someone
// acted on it in between, gets it as their own action left it.
func (s *server) writeDetailAfter(w http.ResponseWriter, r *http.Request, c order.Caller, done order.Order, status int) {
	o, history, err := s.svc.Get(r.Context(), c, done.ID)
	if err != nil {
		if order.KindOf(err) != order.KindNotFound {
			s.log.WarnContext(r.Context(), "could not re-read the errand after an action; answering without its history",
				"request_id", order.CorrelationID(r.Context()), "errand_id", done.ID, "err", err)
		}
		o, history = done, nil
	}
	writeJSON(w, status, toDetail(o, history, c, s.svc.Now()))
}

// writeList answers with a list of errands as c sees them.
func (s *server) writeList(w http.ResponseWriter, c order.Caller, orders []order.Order) {
	now := s.svc.Now()
	writeJSON(w, http.StatusOK, gen.ErrandList{Items: toErrands(orders, c, now), ServerTime: now.UTC()})
}

// writeServiceError maps a service error to its status by kind, with the
// error's caller-safe message. Anything that is not a domain error is a 500
// whose cause goes to the log, never to the client.
func (s *server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	status := statusFor(order.KindOf(err))
	switch status {
	case http.StatusInternalServerError:
		s.log.ErrorContext(ctx, "request failed", "request_id", order.CorrelationID(ctx), "err", err)
		writeError(w, status, msgInternal)
		return
	case http.StatusServiceUnavailable:
		s.log.WarnContext(ctx, "request failed: a dependency is unavailable",
			"request_id", order.CorrelationID(ctx), "err", err)
	}
	writeError(w, status, order.MessageOf(err))
}

func statusFor(k order.Kind) int {
	switch k {
	case order.KindValidation:
		return http.StatusBadRequest
	case order.KindForbidden:
		return http.StatusForbidden
	case order.KindNotFound:
		return http.StatusNotFound
	case order.KindConflict:
		return http.StatusConflict
	case order.KindInsufficientCredits:
		return http.StatusUnprocessableEntity
	case order.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
