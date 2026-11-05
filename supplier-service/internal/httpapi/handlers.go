// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Delete forwards the token authenticate verified to order-service,
//   instead of copying the raw Authorization header. Create and Update read
//   the body through a 64 KiB cap (413 over it).
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	// AI-generated (edited by James Teo).
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"foc/supplier-service/internal/auth"
	// AI-generated (edited by James Teo).
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/supplier"
)

// maxBodyBytes caps a create or update body. A valid supplier is well under
// 2 KiB, so 64 KiB bounds the memory one write can take without refusing any.
const maxBodyBytes = 64 << 10

type Handler struct {
	svc *supplier.Service
}

func NewHandler(svc *supplier.Service) *Handler {
	return &Handler{svc: svc}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// List handles GET /suppliers (FR F2.1.1, F2.1.3).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	filter := supplier.ListFilter{
		Category: r.URL.Query().Get("category"),
		Search:   r.URL.Query().Get("q"),
	}
	suppliers, err := h.svc.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list suppliers")
		return
	}
	writeJSON(w, http.StatusOK, toResponseList(suppliers))
}

// Get handles GET /suppliers/{id} (FR F2.1.2).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := h.svc.Get(r.Context(), id)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toResponse(s))
}

// Create handles POST /suppliers (FR F2.2, admin-only).
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req supplierRequest
	if !decodeBody(w, r, &req) {
		return
	}

	created, err := h.svc.AdminCreate(r.Context(), auditContext(r), req.toDomain())
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(created))
}

// Update handles PUT /suppliers/{id} (FR F2.2, admin-only).
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req supplierRequest
	if !decodeBody(w, r, &req) {
		return
	}

	updated, err := h.svc.AdminUpdate(r.Context(), auditContext(r), id, req.toDomain())
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toResponse(updated))
}

// AI-generated (edited by James Teo).
// Delete handles DELETE /suppliers/{id} (FR F2.2.3, admin-only). See the
// comment on supplier.Service.Delete for the unfinished-errand check and
// why this is a soft delete.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// order-service answers the errand check only for an ADMIN's token, so the
	// token authenticate verified goes along with it.
	ctx := r.Context()
	if token, ok := auth.TokenFromContext(ctx); ok {
		ctx = orderclient.WithToken(ctx, "Bearer "+token)
	}
	err := h.svc.AdminDelete(ctx, auditContext(r), id)
	if handleServiceError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeBody reads the whole request body, through a 64 KiB cap, and then
// unmarshals it into dst. A body over the cap is 413 and one that is not a
// single valid JSON value is 400; either way it writes the answer and reports
// false.
//
// The body is read in full before it is parsed: json.Decoder stops after the
// first value, so a valid supplier followed by 70 KiB of whitespace would
// otherwise be saved without the cap ever being reached.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// Health handles GET /health for container readiness checks.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleServiceError writes the appropriate HTTP status for a service
// error and reports whether it wrote a response (i.e. err != nil).
func handleServiceError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, supplier.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, supplier.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, supplier.ErrDuplicate):
		writeError(w, http.StatusConflict, err.Error())
	// AI-generated (edited by James Teo).
	case errors.Is(err, supplier.ErrInUse):
		writeError(w, http.StatusConflict, err.Error())
	default:
		// AI-generated (edited by James Teo).
		// The client only gets a generic message, so this line is the one
		// place the cause shows up (e.g. order-service refusing the token).
		log.Printf("internal server error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}

// auditContext says who the admin write on r is attributed to: the verified
// caller authenticate put on the context (D-041, never a header) and the
// request id, which ties the row to the access log.
func auditContext(r *http.Request) supplier.AuditContext {
	ac := supplier.AuditContext{CorrelationID: middleware.GetReqID(r.Context())}
	if caller, ok := auth.CallerFromContext(r.Context()); ok {
		ac.ActorID = caller.UserID.String()
		ac.ActorRole = caller.Role
	}
	return ac
}

// AuditLog handles GET /suppliers/audit-log, admin-only (D-110): one page of the
// admin audit log, newest first. Filters: action, outcome, actor_id, target_id,
// from and to (RFC 3339 or YYYY-MM-DD, "to" exclusive); limit (at most 50) and
// offset page it (D-104).
func (h *Handler) AuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := supplier.AuditFilter{
		Action:   q.Get("action"),
		Outcome:  q.Get("outcome"),
		ActorID:  q.Get("actor_id"),
		TargetID: q.Get("target_id"),
	}
	var err error
	if f.Limit, err = intParam(q.Get("limit")); err != nil {
		writeError(w, http.StatusBadRequest, "limit must be a whole number")
		return
	}
	if f.Offset, err = intParam(q.Get("offset")); err != nil {
		writeError(w, http.StatusBadRequest, "offset must be a whole number")
		return
	}
	if f.From, err = timeParam(q.Get("from")); err != nil {
		writeError(w, http.StatusBadRequest, "from must be an RFC 3339 time or YYYY-MM-DD")
		return
	}
	if f.To, err = timeParam(q.Get("to")); err != nil {
		writeError(w, http.StatusBadRequest, "to must be an RFC 3339 time or YYYY-MM-DD")
		return
	}
	page, err := h.svc.AuditLog(r.Context(), f)
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toAuditPageResponse(page))
}

// intParam parses an optional whole-number query value; empty is 0, which the
// service reads as "the default".
func intParam(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("not a whole number: %w", err)
	}
	return n, nil
}

// timeParam parses an optional time: RFC 3339, or a bare date read as midnight
// UTC. Empty is nil.
func timeParam(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			t = t.UTC()
			return &t, nil
		}
	}
	return nil, fmt.Errorf("not a time: %q", v)
}
