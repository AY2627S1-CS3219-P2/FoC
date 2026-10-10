// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated authentication endpoint handlers and their narrow dependencies.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"foc/user-service/internal/session"
	"foc/user-service/internal/user"
)

// Registrar creates an account from registration credentials.
type Registrar interface {
	Register(context.Context, string, string, string) (*user.User, error)
}
type OTPResender interface {
	ResendOTP(context.Context, string) error
}
type OTPVerifier interface {
	VerifyOTP(context.Context, string, string) error
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
	OTPResender    OTPResender
	OTPVerifier    OTPVerifier
	LoginService   LoginService
	Refresher      Refresher
	AccessVerifier session.AccessTokenVerifier
	Logoutter      Logoutter
	Logger         *slog.Logger
}

// AuthHandler serves account-registration and session endpoints.
type AuthHandler struct{ deps AuthDependencies }

// NewAuthHandler constructs an authentication handler with its required operations.
func NewAuthHandler(deps AuthDependencies) AuthHandler { return AuthHandler{deps: deps} }

// Register handles account registration.
func (h AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request RegisterRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.Email == "" || request.Username == "" || request.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email, username, and password are required"})
		return
	}
	if h.deps.Registrar == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("registrar is required"), "registration unavailable")
		return
	}
	if _, err := h.deps.Registrar.Register(r.Context(), request.Email, request.Username, request.Password); err != nil {
		if errors.Is(err, user.ErrInvalidUsername) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username must be at most 128 characters and contain only alphanumeric characters"})
			// AI-generated (edited by ZI YANG): report the recorded database-width validation as a client error.
		} else if errors.Is(err, user.ErrEmailTooLong) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email must be at most 255 characters"})
		} else if errors.Is(err, user.ErrInvalidEmail) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email must end with '@u.nus.edu'"})
		} else if errors.Is(err, user.ErrInvalidPassword) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be 8-128 characters and contain uppercase, lowercase, and digit characters"})
		} else if errors.Is(err, user.ErrDuplicateEmail) || errors.Is(err, user.ErrDuplicateUsername) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "registration unavailable")
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h AuthHandler) ResendOTP(w http.ResponseWriter, r *http.Request) {
	var request OTPEmailRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.Email == "" {
		writeJSON(w, 400, map[string]string{"error": "email is required"})
		return
	}
	if h.deps.OTPResender == nil {
		writeError(r.Context(), w, h.deps.Logger, 500, errors.New("OTP resender is required"), "OTP resend unavailable")
		return
	}
	h.writeOTPError(w, r, h.deps.OTPResender.ResendOTP(r.Context(), request.Email), "OTP resend unavailable")
}

func (h AuthHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var request VerifyOTPRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.Email == "" || request.OTP == "" {
		writeJSON(w, 400, map[string]string{"error": "email and otp are required"})
		return
	}
	if h.deps.OTPVerifier == nil {
		writeError(r.Context(), w, h.deps.Logger, 500, errors.New("OTP verifier is required"), "OTP verification unavailable")
		return
	}
	h.writeOTPError(w, r, h.deps.OTPVerifier.VerifyOTP(r.Context(), request.Email, request.OTP), "OTP verification unavailable")
}

func (h AuthHandler) writeOTPError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	if err == nil {
		writeJSON(w, 200, map[string]any{})
		return
	}
	switch {
	case errors.Is(err, user.ErrInvalidEmail), errors.Is(err, user.ErrInvalidOTP):
		writeJSON(w, 400, map[string]string{"error": "invalid OTP"})
	case errors.Is(err, user.ErrNotFound):
		writeJSON(w, 404, map[string]string{"error": "OTP not found"})
	case errors.Is(err, user.ErrAccountAlreadyVerified):
		writeJSON(w, 409, map[string]string{"error": "Account is already verified"})
	case errors.Is(err, user.ErrOTPRequestLimit):
		writeJSON(w, 429, map[string]string{"error": "OTP request limit exceeded"})
	default:
		writeError(r.Context(), w, h.deps.Logger, 500, err, fallback)
	}
}

// Login handles credential login.
func (h AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.Identifier == "" || request.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "identifier and password are required"})
		return
	}
	if h.deps.LoginService == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("login service is required"), "authentication unavailable")
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
		} else if errors.Is(err, user.ErrAccountUnverified) {
			// AI Assistance Disclosure: Codex (GPT-5), 2026-10-09 — exposes the
			// owner-recorded frontend OTP-routing signal without issuing tokens. Author review: ZI YANG - validated Correctness.
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "ACCOUNT_UNVERIFIED"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "authentication unavailable")
		}
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

// Refresh handles refresh-token rotation.
func (h AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var request RefreshRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refreshToken is required"})
		return
	}
	if h.deps.Refresher == nil {
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("refresh service is required"), "refresh unavailable")
		return
	}
	pair, err := h.deps.Refresher.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		if errors.Is(err, user.ErrRefreshInProgress) {
			// AI-generated (edited by ZI YANG): the recorded Redis debounce lock reports active same-token work without touching PostgreSQL.
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "refresh already in progress"})
		} else if errors.Is(err, user.ErrSessionCompromised) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "ErrSessionCompromised"})
		} else if errors.Is(err, user.ErrSessionNotFound) || errors.Is(err, user.ErrAccountSuspended) {
			// AI-generated (edited by ZI YANG): only recorded authentication outcomes receive 401.
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "refresh unavailable")
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
		writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, errors.New("logout dependencies are required"), "logout unavailable")
		return
	}
	accessClaims, err := h.deps.AccessVerifier.VerifyAccess(r.Context(), rawAccessToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	var request LogoutRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refreshToken is required"})
		return
	}
	if err := h.deps.Logoutter.Logout(r.Context(), accessClaims, request.RefreshToken); err != nil {
		// AI-generated (edited by ZI YANG): expose the recorded mismatched-session client error.
		if errors.Is(err, session.ErrSessionOwnershipMismatch) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mismatched session"})
		} else {
			writeError(r.Context(), w, h.deps.Logger, http.StatusInternalServerError, err, "logout failed")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
