// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated authentication endpoint handlers and their narrow dependencies.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"foc/user-service/internal/session"
	"foc/user-service/internal/user"
)

// Registrar creates an account from registration credentials.
type Registrar interface {
	Register(context.Context, string, string, string) (*user.User, error)
}

// LoginService authenticates an account and persists its refresh session.
type LoginService interface {
	Login(context.Context, string, string) (session.TokenPair, error)
}

// Refresher rotates a valid refresh-token session.
type Refresher interface {
	Refresh(context.Context, string) (session.TokenPair, error)
}

// Logoutter invalidates a verified access token and refresh session.
type Logoutter interface {
	Logout(context.Context, session.AccessTokenClaims, string) error
}

// AuthDependencies contains only the operations required by AuthHandler.
type AuthDependencies struct {
	Registrar      Registrar
	LoginService   LoginService
	Refresher      Refresher
	AccessVerifier session.AccessTokenVerifier
	Logoutter      Logoutter
}

// AuthHandler serves account-registration and session endpoints.
type AuthHandler struct{ deps AuthDependencies }

// NewAuthHandler constructs an authentication handler with its required operations.
func NewAuthHandler(deps AuthDependencies) AuthHandler { return AuthHandler{deps: deps} }

// Register handles account registration.
func (h AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.Email == "" || request.Username == "" || request.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email, username, and password are required"})
		return
	}
	if !isNUSStudentEmail(request.Email) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email must end with '@u.nus.edu'"})
		return
	}
	if h.deps.Registrar == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration unavailable"})
		return
	}
	request.Email = normalizeEmail(request.Email)
	if _, err := h.deps.Registrar.Register(r.Context(), request.Email, request.Username, request.Password); err != nil {
		if errors.Is(err, user.ErrInvalidPassword) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be 8-128 characters and contain uppercase, lowercase, and digit characters"})
		} else if errors.Is(err, user.ErrDuplicateEmail) || errors.Is(err, user.ErrDuplicateUsername) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration unavailable"})
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// Login handles credential login.
func (h AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.Identifier == "" || request.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "identifier and password are required"})
		return
	}
	if h.deps.LoginService == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "authentication unavailable"})
		return
	}
	request.Identifier = strings.TrimSpace(request.Identifier)
	if strings.Contains(request.Identifier, "@") {
		request.Identifier = normalizeEmail(request.Identifier)
	}
	pair, err := h.deps.LoginService.Login(r.Context(), request.Identifier, request.Password)
	if err != nil {
		if errors.Is(err, user.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		} else if errors.Is(err, user.ErrAccountSuspended) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account suspended"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "authentication unavailable"})
		}
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

// Refresh handles refresh-token rotation.
func (h AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var request RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refreshToken is required"})
		return
	}
	if h.deps.Refresher == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refresh unavailable"})
		return
	}
	pair, err := h.deps.Refresher.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		if errors.Is(err, user.ErrSessionCompromised) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "ErrSessionCompromised"})
		} else {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		}
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

// Logout handles access-token and refresh-session invalidation.
func (h AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	rawAccessToken, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	if h.deps.AccessVerifier == nil || h.deps.Logoutter == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout unavailable"})
		return
	}
	accessClaims, err := h.deps.AccessVerifier.VerifyAccess(r.Context(), rawAccessToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	var request LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refreshToken is required"})
		return
	}
	if err := h.deps.Logoutter.Logout(r.Context(), accessClaims, request.RefreshToken); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isNUSStudentEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.LastIndexByte(email, '@')
	return at > 0 && strings.EqualFold(email[at:], "@u.nus.edu")
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
