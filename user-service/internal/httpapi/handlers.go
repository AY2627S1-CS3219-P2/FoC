package httpapi

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

// Registrar creates an account from registration credentials.
type Registrar interface {
	Register(ctx context.Context, email, username, password string) (*user.User, error)
}

// ProfileGetter retrieves a public user profile by ID.
type ProfileGetter interface {
	GetByID(ctx context.Context, uid uuid.UUID) (*user.User, error)
}
type AccountStatusUpdater interface {
	UpdateAccountStatus(context.Context, uuid.UUID, user.AccountStatus, time.Time) error
}
type ProfileUpdater interface {
	UpdateProfile(context.Context, uuid.UUID, string, string, string) (*user.User, error)
}

// LoginService authenticates an account and persists its refresh-token session.
type LoginService interface {
	Login(ctx context.Context, identifier, password string) (user.TokenPair, error)
}

// Refresher rotates a valid refresh-token session and returns a new token pair.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (user.TokenPair, error)
}

// Logoutter invalidates the verified access token and refresh session.
type Logoutter interface {
	Logout(ctx context.Context, access user.AccessTokenClaims, refreshToken string) error
}

// JWKSProvider returns the public JSON Web Key Set used by the API gateway.
type JWKSProvider interface {
	JWKS() ([]byte, error)
}

// HealthCheck reports whether the service dependencies are healthy.
type HealthCheck func(ctx context.Context) error

// Dependencies contains the operations required by the HTTP transport.
type Dependencies struct {
	Registrar      Registrar
	ProfileGetter  ProfileGetter
	StatusUpdater  AccountStatusUpdater
	ProfileUpdater ProfileUpdater
	TokenVerifier  TokenVerifier
	LoginService   LoginService
	Refresher      Refresher
	AccessVerifier user.AccessTokenVerifier
	Logoutter      Logoutter
	JWKSProvider   JWKSProvider
	HealthCheck    HealthCheck
}

type handler struct {
	deps Dependencies
}

func (h handler) health(w http.ResponseWriter, r *http.Request) {
	if h.deps.HealthCheck != nil && h.deps.HealthCheck(r.Context()) != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h handler) register(w http.ResponseWriter, r *http.Request) {
	var request RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.Email == "" || request.Username == "" || len(request.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email, username, and an 8-character password are required"})
		return
	}
	if h.deps.Registrar == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration unavailable"})
		return
	}
	if _, err := h.deps.Registrar.Register(r.Context(), request.Email, request.Username, request.Password); err != nil {
		switch {
		case errors.Is(err, user.ErrDuplicateEmail), errors.Is(err, user.ErrDuplicateUsername):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration unavailable"})
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h handler) profile(w http.ResponseWriter, r *http.Request) {
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
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile unavailable"})
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	writeJSON(w, http.StatusOK, UserResponse{UID: account.UID.String(), Username: account.Username, AccountRole: string(account.AccountRole), AccountStatus: string(account.AccountStatus), DateCreated: account.DateCreated})
}

func (h handler) updateStatus(w http.ResponseWriter, r *http.Request) {
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

func (h handler) updateProfile(w http.ResponseWriter, r *http.Request) {
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
	account, err := h.deps.ProfileUpdater.UpdateProfile(r.Context(), uid, request.Username, "", request.Password)
	if err != nil {
		if errors.Is(err, user.ErrDuplicateUsername) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "username already taken"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, UserResponse{UID: account.UID.String(), Username: account.Username, AccountRole: string(account.AccountRole), AccountStatus: string(account.AccountStatus), DateCreated: account.DateCreated})
}

func (h handler) login(w http.ResponseWriter, r *http.Request) {
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

	pair, err := h.deps.LoginService.Login(r.Context(), request.Identifier, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, user.ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		case errors.Is(err, user.ErrAccountSuspended):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account suspended"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "authentication unavailable"})
		}
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

func (h handler) refresh(w http.ResponseWriter, r *http.Request) {
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
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken})
}

func (h handler) logout(w http.ResponseWriter, r *http.Request) {
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

func (h handler) jwks(w http.ResponseWriter, _ *http.Request) {
	if h.deps.JWKSProvider == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWKS unavailable"})
		return
	}
	payload, err := h.deps.JWKSProvider.JWKS()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWKS unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (handler) notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "endpoint not implemented"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
