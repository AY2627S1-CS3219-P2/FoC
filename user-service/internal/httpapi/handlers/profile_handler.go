// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated profile and account-status endpoint handlers and their narrow dependencies.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"foc/user-service/internal/user"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// auditContext says who the admin action on r is attributed to: the verified
// principal (D-041, never a header) and the request id, which ties the audit row
// to the access log.
func auditContext(r *http.Request, p Principal) user.AuditContext {
	return user.AuditContext{
		ActorID:       p.UserID.String(),
		ActorRole:     string(p.Role),
		CorrelationID: middleware.GetReqID(r.Context()),
	}
}

// AI-generated (edited by ZI YANG): avoids describing the domain account as a
// public response, because it includes fields selected later by the handler.
// ProfileGetter retrieves an account by ID.
type ProfileGetter interface {
	GetByID(context.Context, uuid.UUID) (*user.User, error)
}

// AccountStatusUpdater changes an account status for an admin; the audit
// context says which admin, and the change is logged with it (D-106).
type AccountStatusUpdater interface {
	AdminUpdateAccountStatus(context.Context, user.AuditContext, uuid.UUID, user.AccountStatus) error
}

// ProfileUpdater changes mutable profile fields.
type ProfileUpdater interface {
	UpdateProfile(context.Context, uuid.UUID, string, string, string, string) (*user.User, error)
}

// AdminProfileUpdater changes another account's mutable profile fields for an
// admin, and logs the change under the audit context (D-106).
type AdminProfileUpdater interface {
	AdminUpdateProfile(context.Context, user.AuditContext, uuid.UUID, string, string, string) (*user.User, error)
}

// ProfileDependencies contains only the operations required by ProfileHandler.
type ProfileDependencies struct {
	ProfileGetter       ProfileGetter
	StatusUpdater       AccountStatusUpdater
	ProfileUpdater      ProfileUpdater
	AdminProfileUpdater AdminProfileUpdater
	Logger              *slog.Logger
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
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("profile getter is required"), "profile unavailable")
		return
	}
	account, err := h.deps.ProfileGetter.GetByID(r.Context(), uid)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "profile unavailable")
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
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.Status != user.AccountStatusActive && request.Status != user.AccountStatusSuspended {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account status"})
		return
	}
	if h.deps.StatusUpdater == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("account status updater is required"), "account status unavailable")
		return
	}
	if err := h.deps.StatusUpdater.AdminUpdateAccountStatus(r.Context(), auditContext(r, p), uid, request.Status); err != nil {
		// AI-generated (edited by ZI YANG): expose a missing account as the recorded 404 response.
		if errors.Is(err, user.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			// AI-generated (edited by ZI YANG): maps the recorded last-active-admin guard to its public conflict response.
			// D-118 and D-119: no admin account is suspended, by another admin or by its owner.
		} else if errors.Is(err, user.ErrCannotSuspendSelf) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "you cannot suspend your own account"})
		} else if errors.Is(err, user.ErrCannotSuspendAdmin) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "an admin account cannot be suspended"})
		} else if errors.Is(err, user.ErrLastAdmin) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot suspend the last active admin"})
		} else if errors.Is(err, user.ErrAccountUnverified) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "account is not verified"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "account status unavailable")
		}
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
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if h.deps.ProfileUpdater == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("profile updater is required"), "profile unavailable")
		return
	}
	var account *user.User
	if p.Role == user.AccountRoleAdmin && p.UserID != uid {
		if h.deps.AdminProfileUpdater == nil {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("admin profile updater is required"), "profile unavailable")
			return
		}
		account, err = h.deps.AdminProfileUpdater.AdminUpdateProfile(r.Context(), auditContext(r, p), uid, request.Username, request.PhoneNum, request.NewPassword)
	} else {
		account, err = h.deps.ProfileUpdater.UpdateProfile(r.Context(), uid, request.Username, request.PhoneNum, request.CurrentPassword, request.NewPassword)
	}
	if err != nil {
		if errors.Is(err, user.ErrInvalidUsername) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username must be at most 128 characters and contain only alphanumeric characters"})
			// AI-generated (edited by ZI YANG): report the recorded database-width validation as a client error.
		} else if errors.Is(err, user.ErrInvalidPhone) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone number must be at most 20 characters"})
		} else if errors.Is(err, user.ErrInvalidCurrentPassword) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid current password"})
		} else if errors.Is(err, user.ErrInvalidPassword) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be 8-128 characters and contain uppercase, lowercase, and digit characters"})
		} else if errors.Is(err, user.ErrDuplicateUsername) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "username already taken"})
			// AI-generated (edited by ZI YANG): expose a missing account as the recorded 404 response.
		} else if errors.Is(err, user.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "profile unavailable")
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
