// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated profile and account-status endpoint handlers and their narrow dependencies.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"foc/user-service/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ProfileGetter retrieves a public profile by account ID.
type ProfileGetter interface {
	GetByID(context.Context, uuid.UUID) (*user.User, error)
}

// AccountStatusUpdater changes an account status at a supplied time.
type AccountStatusUpdater interface {
	UpdateAccountStatus(context.Context, uuid.UUID, user.AccountStatus, time.Time) error
}

// ProfileUpdater changes mutable profile fields.
type ProfileUpdater interface {
	UpdateProfile(context.Context, uuid.UUID, string, string, string) (*user.User, error)
}

// ProfileDependencies contains only the operations required by ProfileHandler.
type ProfileDependencies struct {
	ProfileGetter  ProfileGetter
	StatusUpdater  AccountStatusUpdater
	ProfileUpdater ProfileUpdater
}

// ProfileHandler serves profile and account-status endpoints.
type ProfileHandler struct{ deps ProfileDependencies }

// NewProfileHandler constructs a profile handler with its required operations.
func NewProfileHandler(deps ProfileDependencies) ProfileHandler { return ProfileHandler{deps: deps} }

// Profile handles role-based profile lookup.
func (h ProfileHandler) Profile(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if h.deps.ProfileGetter == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile unavailable"})
		return
	}
	account, err := h.deps.ProfileGetter.GetByID(r.Context(), uid)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile unavailable"})
		}
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// AI-generated (edited by ZI YANG): select the response fields from the verified role.
	if principal.Role == user.AccountRoleAdmin {
		writeJSON(w, http.StatusOK, userResponse(account))
		return
	}
	writeJSON(w, http.StatusOK, restrictedUserResponse(account))
}

// UpdateStatus handles an administrative account-status update.
func (h ProfileHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok || p.Role != user.AccountRoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user ID"})
		return
	}
	var request UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.Status != user.AccountStatusActive && request.Status != user.AccountStatusSuspended {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account status"})
		return
	}
	if h.deps.StatusUpdater == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account status unavailable"})
		return
	}
	if err := h.deps.StatusUpdater.UpdateAccountStatus(r.Context(), uid, request.Status, time.Now().UTC()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account status unavailable"})
		return
	}
	w.WriteHeader(http.StatusOK)
}

// UpdateProfile handles a self-or-admin profile update.
func (h ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user ID"})
		return
	}
	if p.UserID != uid && p.Role != user.AccountRoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var request UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if h.deps.ProfileUpdater == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile unavailable"})
		return
	}
	account, err := h.deps.ProfileUpdater.UpdateProfile(r.Context(), uid, request.Username, request.PhoneNum, request.Password)
	if err != nil {
		if errors.Is(err, user.ErrInvalidUsername) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username must be at most 128 characters and contain only alphanumeric characters"})
		} else if errors.Is(err, user.ErrInvalidPassword) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be 8-128 characters and contain uppercase, lowercase, and digit characters"})
		} else if errors.Is(err, user.ErrDuplicateUsername) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "username already taken"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile unavailable"})
		}
		return
	}
	writeJSON(w, http.StatusOK, userResponse(account))
}

func userResponse(account *user.User) UserResponse {
	return UserResponse{
		UID:           account.UID.String(),
		Username:      account.Username,
		Email:         account.Email,
		PhoneNum:      account.PhoneNum,
		AccountRole:   string(account.AccountRole),
		AccountStatus: string(account.AccountStatus),
		DateCreated:   account.DateCreated,
	}
}

func restrictedUserResponse(account *user.User) RestrictedUserResponse {
	return RestrictedUserResponse{
		UID:      account.UID.String(),
		Username: account.Username,
		Email:    account.Email,
		PhoneNum: account.PhoneNum,
	}
}
