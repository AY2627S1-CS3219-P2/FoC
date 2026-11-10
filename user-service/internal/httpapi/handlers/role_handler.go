// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Approved administrator role-change HTTP contract and error mapping.
// Author review: ZI YANG - reviewed and verified

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"foc/user-service/internal/user"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// RoleUpdater changes a role using the verified actor's audit context.
type RoleUpdater interface {
	AdminUpdateRole(context.Context, user.AuditContext, uuid.UUID, user.AccountRole) error
}

// UpdateRoleRequest is the approved role-change JSON body.
type UpdateRoleRequest struct {
	Role user.AccountRole `json:"role"`
}

// UpdateRole handles PATCH /api/v1/users/{uid}/role.
func (h AdminHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user ID"})
		return
	}
	var body UpdateRoleRequest
	if !decodeRoleChangeBody(w, r, &body) {
		return
	}
	if h.deps.RoleUpdater == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("role updater is required"), "account role unavailable")
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	if err := h.deps.RoleUpdater.AdminUpdateRole(r.Context(), auditContext(r, principal), uid, body.Role); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, user.ErrInvalidRole):
			status = http.StatusBadRequest
		case errors.Is(err, user.ErrAdminRequired), errors.Is(err, user.ErrCannotDemoteSelf):
			status = http.StatusForbidden
		case errors.Is(err, user.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, user.ErrLastActiveAdmin), errors.Is(err, user.ErrInactivePromotion):
			status = http.StatusConflict
		}
		if status == http.StatusInternalServerError {
			writeError(r.Context(), w, h.deps.Logger, status, err, "account role unavailable")
			return
		}
		// Expose only domain error messages, never repository wrappers.
		for _, refusal := range []error{user.ErrInvalidRole, user.ErrAdminRequired, user.ErrCannotDemoteSelf, user.ErrNotFound, user.ErrLastActiveAdmin, user.ErrInactivePromotion} {
			if errors.Is(err, refusal) {
				writeJSON(w, status, map[string]string{"error": refusal.Error()})
				return
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeRoleChangeBody consumes exactly one JSON value, including trailing
// whitespace, so a valid prefix cannot bypass the body limit or hide bad JSON.
func decodeRoleChangeBody(w http.ResponseWriter, r *http.Request, body *UpdateRoleRequest) bool {
	r.Body = http.MaxBytesReader(innermostWriter(w), r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(body)
	if err == nil {
		var trailing any
		err = decoder.Decode(&trailing)
		if err == io.EOF {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	return false
}
