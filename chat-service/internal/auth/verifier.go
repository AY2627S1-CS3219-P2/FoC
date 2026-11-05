package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrMalformed        = errors.New("auth: malformed token")
	ErrInvalidSignature = errors.New("auth: invalid signature")
	ErrExpired          = errors.New("auth: token expired")
	ErrWrongType        = errors.New("auth: not an access token")
	ErrKeysUnavailable  = errors.New("auth: signing keys unavailable")
	ErrRevoked          = errors.New("auth: token revoked or account suspended")
	ErrRedisUnreachable = errors.New("auth: redis unreachable")
)

const (
	clockSkew          = 5 * time.Second
	issuer             = "campusrun-user-service"
	audience           = "campusrun-api"
	minRefreshInterval = 30 * time.Second
	revocationTimeout  = 500 * time.Millisecond
	blocklistPrefix    = "jti:"
	suspensionPrefix   = "suspended:uid:"
)

type Claims struct {
	UserID   uuid.UUID
	Role     string
	TokenID  string
	IssuedAt time.Time
}

type Verifier struct {
	jwksURL     string
	httpClient  *http.Client
	authRedis   redis.UniversalClient
	fetchMu     sync.Mutex
	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	lastErr     error
}

func NewVerifier(jwksURL string, authRedis redis.UniversalClient) *Verifier {
	return &Verifier{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		authRedis:  authRedis,
		keys:       make(map[string]*rsa.PublicKey),
	}
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid"`
}

type jwtPayload struct {
	Iss string `json:"iss"`
	Aud string `json:"aud"`
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
	Jti string `json:"jti"`
	Typ string `json:"typ"`
	Rol string `json:"role"`
}

func (v *Verifier) Verify(ctx context.Context, tokenString string) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrMalformed
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrMalformed
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, ErrMalformed
	}
	if header.Alg != "RS256" || header.Kid == "" {
		return nil, ErrInvalidSignature
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrMalformed
	}
	var payload jwtPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, ErrMalformed
	}

	if payload.Iss != issuer || payload.Aud != audience {
		return nil, ErrInvalidSignature
	}
	if payload.Typ != "" && payload.Typ != "access" {
		return nil, ErrWrongType
	}

	now := time.Now()
	if payload.Exp != 0 && now.After(time.Unix(payload.Exp, 0).Add(clockSkew)) {
		return nil, ErrExpired
	}

	userID, err := uuid.Parse(payload.Sub)
	if err != nil || userID == uuid.Nil {
		return nil, ErrMalformed
	}

	claims := &Claims{
		UserID:   userID,
		Role:     payload.Rol,
		TokenID:  payload.Jti,
		IssuedAt: time.Unix(payload.Iat, 0),
	}

	// Verify cryptographic signature
	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return nil, err
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}))
	_, err = parser.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	// Check revocation in shared Redis (D-041, D-054)
	if v.authRedis != nil {
		revoked, err := v.isRevoked(ctx, claims)
		if err != nil {
			return nil, err
		}
		if revoked {
			return nil, ErrRevoked
		}
	}

	return claims, nil
}

func (v *Verifier) isRevoked(ctx context.Context, c *Claims) (bool, error) {
	rctx, cancel := context.WithTimeout(ctx, revocationTimeout)
	defer cancel()

	vals, err := v.authRedis.MGet(rctx, blocklistPrefix+c.TokenID, suspensionPrefix+c.UserID.String()).Result()
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRedisUnreachable, err)
	}
	if len(vals) != 2 {
		return false, errors.New("auth: unexpected mget response from redis")
	}

	// 1. Check if token JTI is blocklisted (logout)
	if vals[0] != nil {
		return true, nil
	}

	// 2. Check if user account is suspended
	if vals[1] != nil {
		raw, ok := vals[1].(string)
		if !ok {
			return true, nil
		}
		suspendedAt, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			// Unreadable suspension timestamp -> fail closed (revoked)
			return true, nil
		}
		if !c.IssuedAt.After(suspendedAt) {
			return true, nil
		}
	}

	return false, nil
}

func (v *Verifier) IsUserSuspended(ctx context.Context, userID uuid.UUID) (bool, error) {
	if v.authRedis == nil {
		return false, nil
	}
	rctx, cancel := context.WithTimeout(ctx, revocationTimeout)
	defer cancel()

	exists, err := v.authRedis.Exists(rctx, suspensionPrefix+userID.String()).Result()
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRedisUnreachable, err)
	}
	return exists > 0, nil
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	v.mu.RUnlock()
	if ok {
		return key, nil
	}

	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()

	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()
	if ok {
		return key, nil
	}

	if time.Since(v.lastAttempt) < minRefreshInterval && v.lastErr != nil {
		return nil, v.lastErr
	}

	if err := v.refresh(ctx); err != nil {
		return nil, err
	}

	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()
	if ok {
		return key, nil
	}
	return nil, ErrInvalidSignature
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrKeysUnavailable, err)
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		v.lastAttempt = time.Now()
		v.lastErr = fmt.Errorf("%w: %v", ErrKeysUnavailable, err)
		return v.lastErr
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		v.lastAttempt = time.Now()
		v.lastErr = fmt.Errorf("%w: %s returned %d", ErrKeysUnavailable, v.jwksURL, resp.StatusCode)
		return v.lastErr
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		v.lastAttempt = time.Now()
		v.lastErr = fmt.Errorf("%w: decode jwks: %v", ErrKeysUnavailable, err)
		return v.lastErr
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pk, err := parseRSAPublicKey(k.N, k.E)
		if err == nil {
			newKeys[k.Kid] = pk
		}
	}

	if len(newKeys) == 0 {
		v.lastAttempt = time.Now()
		v.lastErr = fmt.Errorf("%w: no valid RSA keys found in jwks", ErrKeysUnavailable)
		return v.lastErr
	}

	v.mu.Lock()
	v.keys = newKeys
	v.lastAttempt = time.Now()
	v.lastErr = nil
	v.mu.Unlock()

	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("empty n or e")
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

type contextKey struct{}

var callerCtxKey = contextKey{}

func WithCaller(ctx context.Context, caller chat.Caller) context.Context {
	return context.WithValue(ctx, callerCtxKey, caller)
}

func CallerFromContext(ctx context.Context) (chat.Caller, bool) {
	caller, ok := ctx.Value(callerCtxKey).(chat.Caller)
	return caller, ok
}

func RequireToken(v *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := v.Verify(r.Context(), tokenString)
			if err != nil {
				if errors.Is(err, ErrRedisUnreachable) {
					http.Error(w, `{"error":"auth verification service unavailable"}`, http.StatusServiceUnavailable)
					return
				}
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusUnauthorized)
				return
			}

			caller := chat.Caller{
				UserID: claims.UserID,
				Role:   claims.Role,
			}
			ctx := WithCaller(r.Context(), caller)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
