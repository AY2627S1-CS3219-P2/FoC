package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"foc/supplier-service/internal/supplier"
)

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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	created, err := h.svc.Create(r.Context(), req.toDomain())
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(created))
}

// Update handles PUT /suppliers/{id} (FR F2.2, admin-only).
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req supplierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	updated, err := h.svc.Update(r.Context(), id, req.toDomain())
	if handleServiceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toResponse(updated))
}

// Delete handles DELETE /suppliers/{id} (FR F2.2.3, admin-only). See the
// comment on supplier.Service.Delete for why this is a soft delete.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.svc.Delete(r.Context(), id)
	if handleServiceError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}
