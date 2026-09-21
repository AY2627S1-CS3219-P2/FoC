// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented the recorded RS256 JWT signing, verification, and JWKS boundary.
// Author review: COMPLETED BY ZI YANG

// Package auth contains the user-service authentication adapters.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"foc/user-service/internal/session"
	"foc/user-service/internal/user"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	// Issuer is the recorded JWT issuer for user-service tokens.
	Issuer = "campusrun-user-service"
	// Audience is the recorded audience for API access tokens.
	Audience = "campusrun-api"
	// ClockSkew is the allowed server-clock drift during claim validation.
	ClockSkew = 5 * time.Second
	// DefaultAccessTokenTTL is the recorded default lifetime for access tokens.
	DefaultAccessTokenTTL = 15 * time.Minute
	// DefaultRefreshTokenTTL is the recorded default lifetime for refresh tokens.
	DefaultRefreshTokenTTL = 7 * 24 * time.Hour
)

// TokenType identifies the two JWT credentials issued by user-service.
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// Key describes one RSA signing key. Retired keys omit PrivateKey but remain
// available for verification and publication in JWKS until their tokens expire.
type Key struct {
	ID         string
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
}

// KeySet holds simultaneously available signing and verification keys.
type KeySet struct {
	keys     map[string]Key
	activeID string
}

// NewKeySet validates and constructs a key set with one active signing key.
func NewKeySet(keys []Key, activeID string) (KeySet, error) {
	if len(keys) == 0 {
		return KeySet{}, errors.New("JWT key set must contain at least one key")
	}
	if activeID == "" {
		return KeySet{}, errors.New("JWT active key ID is required")
	}

	keySet := KeySet{keys: make(map[string]Key, len(keys)), activeID: activeID}
	for _, key := range keys {
		if key.ID == "" || key.PublicKey == nil {
			return KeySet{}, errors.New("JWT keys require an ID and public key")
		}
		if _, exists := keySet.keys[key.ID]; exists {
			return KeySet{}, fmt.Errorf("duplicate JWT key ID %q", key.ID)
		}
		keySet.keys[key.ID] = key
	}

	active, ok := keySet.keys[activeID]
	if !ok || active.PrivateKey == nil {
		return KeySet{}, fmt.Errorf("active JWT key %q must have a private key", activeID)
	}
	return keySet, nil
}

// VerifiedToken contains the claims required by downstream authentication
// boundaries without exposing a vendor JWT type.
type VerifiedToken struct {
	Subject   uuid.UUID
	Role      user.AccountRole
	Type      TokenType
	JTI       uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
	KeyID     string
}

// Service signs and verifies the recorded JWT credential types.
type Service struct {
	keys            KeySet
	now             func() time.Time
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

// NewService constructs a JWT service with an injectable clock for deterministic
// expiry tests.
func NewService(keys KeySet, now func() time.Time) *Service {
	service, err := NewServiceWithTokenTTLs(keys, now, DefaultAccessTokenTTL, DefaultRefreshTokenTTL)
	if err != nil {
		panic(err)
	}
	return service
}

// NewServiceWithTokenTTLs constructs a JWT service using validated configured
// lifetimes for newly issued access and refresh tokens.
func NewServiceWithTokenTTLs(keys KeySet, now func() time.Time, accessTokenTTL, refreshTokenTTL time.Duration) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	if accessTokenTTL <= 0 || refreshTokenTTL <= 0 {
		return nil, errors.New("JWT token TTLs must be positive")
	}
	return &Service{
		keys:            keys,
		now:             now,
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}, nil
}

// IssueAccessToken creates an RS256 access token containing the user's role.
func (s *Service) IssueAccessToken(subject uuid.UUID, role user.AccountRole, ttl time.Duration) (string, error) {
	return s.issue(subject, role, AccessToken, ttl)
}

// IssueRefreshToken creates an RS256 refresh token without an account role.
func (s *Service) IssueRefreshToken(subject uuid.UUID, ttl time.Duration) (string, error) {
	return s.issue(subject, "", RefreshToken, ttl)
}

// Issue implements the domain token issuer using the recorded default token
// lifetimes and returns the refresh metadata required for session persistence.
func (s *Service) Issue(ctx context.Context, account *user.User) (session.TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return session.TokenPair{}, fmt.Errorf("issue JWT session: %w", err)
	}
	if account == nil {
		return session.TokenPair{}, errors.New("JWT account is required")
	}
	accessToken, err := s.IssueAccessToken(account.UID, account.AccountRole, s.accessTokenTTL)
	if err != nil {
		return session.TokenPair{}, err
	}
	refreshToken, err := s.IssueRefreshToken(account.UID, s.refreshTokenTTL)
	if err != nil {
		return session.TokenPair{}, err
	}
	refreshClaims, err := s.Verify(refreshToken)
	if err != nil {
		return session.TokenPair{}, fmt.Errorf("read issued refresh claims: %w", err)
	}
	return session.TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		RefreshJTI:       refreshClaims.JTI,
		RefreshExpiresAt: refreshClaims.ExpiresAt,
	}, nil
}

func (s *Service) issue(subject uuid.UUID, role user.AccountRole, tokenType TokenType, ttl time.Duration) (string, error) {
	if subject == uuid.Nil {
		return "", errors.New("JWT subject is required")
	}
	if ttl <= 0 {
		return "", errors.New("JWT TTL must be positive")
	}
	if tokenType == AccessToken && role == "" {
		return "", errors.New("JWT access token role is required")
	}

	now := s.now().UTC()
	key := s.keys.keys[s.keys.activeID]
	claims := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   subject.String(),
			Audience:  jwt.ClaimStrings{Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
		Type: tokenType,
		Role: role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = key.ID
	raw, err := token.SignedString(key.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return raw, nil
}

// Verify validates an RS256 JWT, its issuer, audience, timing claims, key ID,
// and token-specific claims.
func (s *Service) Verify(raw string) (VerifiedToken, error) {
	var parsed claims
	parser := jwt.NewParser(
		jwt.WithAudience(Audience),
		jwt.WithIssuer(Issuer),
		jwt.WithLeeway(ClockSkew),
		jwt.WithTimeFunc(s.now),
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
	)
	token, err := parser.ParseWithClaims(raw, &parsed, func(token *jwt.Token) (any, error) {
		keyID, ok := token.Header["kid"].(string)
		if !ok || keyID == "" {
			return nil, errors.New("JWT key ID is required")
		}
		key, ok := s.keys.keys[keyID]
		if !ok {
			return nil, errors.New("JWT key is unknown")
		}
		return key.PublicKey, nil
	})
	if err != nil {
		return VerifiedToken{}, fmt.Errorf("verify JWT: %w", err)
	}
	if !token.Valid {
		return VerifiedToken{}, errors.New("JWT is invalid")
	}

	subject, err := uuid.Parse(parsed.Subject)
	if err != nil || subject == uuid.Nil {
		return VerifiedToken{}, errors.New("JWT subject is invalid")
	}
	jti, err := uuid.Parse(parsed.ID)
	if err != nil || jti == uuid.Nil {
		return VerifiedToken{}, errors.New("JWT ID is invalid")
	}
	if parsed.Type != AccessToken && parsed.Type != RefreshToken {
		return VerifiedToken{}, errors.New("JWT type is invalid")
	}
	if parsed.Type == AccessToken && parsed.Role == "" {
		return VerifiedToken{}, errors.New("JWT access token role is required")
	}
	if parsed.Type == RefreshToken && parsed.Role != "" {
		return VerifiedToken{}, errors.New("JWT refresh token must not contain a role")
	}

	keyID, _ := token.Header["kid"].(string)
	return VerifiedToken{
		Subject:   subject,
		Role:      parsed.Role,
		Type:      parsed.Type,
		JTI:       jti,
		IssuedAt:  parsed.IssuedAt.Time,
		ExpiresAt: parsed.ExpiresAt.Time,
		KeyID:     keyID,
	}, nil
}

// VerifyRefresh implements the domain refresh-token boundary while keeping
// JWT library types inside this adapter.
func (s *Service) VerifyRefresh(ctx context.Context, rawToken string) (session.RefreshClaims, error) {
	if err := ctx.Err(); err != nil {
		return session.RefreshClaims{}, fmt.Errorf("verify refresh JWT: %w", err)
	}
	verified, err := s.Verify(rawToken)
	if err != nil {
		return session.RefreshClaims{}, err
	}
	if verified.Type != RefreshToken {
		return session.RefreshClaims{}, errors.New("JWT is not a refresh token")
	}
	return session.RefreshClaims{
		UserID:    verified.Subject,
		JTI:       verified.JTI,
		IssuedAt:  verified.IssuedAt,
		ExpiresAt: verified.ExpiresAt,
	}, nil
}

// VerifyAccess implements the domain access-token verification boundary while
// keeping JWT library types inside this adapter.
func (s *Service) VerifyAccess(ctx context.Context, rawToken string) (session.AccessTokenClaims, error) {
	if err := ctx.Err(); err != nil {
		return session.AccessTokenClaims{}, fmt.Errorf("verify access JWT: %w", err)
	}
	verified, err := s.Verify(rawToken)
	if err != nil {
		return session.AccessTokenClaims{}, err
	}
	if verified.Type != AccessToken {
		return session.AccessTokenClaims{}, errors.New("JWT is not an access token")
	}
	return session.AccessTokenClaims{JTI: verified.JTI, ExpiresAt: verified.ExpiresAt}, nil
}

// JWKS returns the public keys in a self-contained JSON Web Key Set document.
func (s *Service) JWKS() ([]byte, error) {
	keys := make([]jwk, 0, len(s.keys.keys))
	for _, key := range s.keys.keys {
		keys = append(keys, newJWK(key))
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].KeyID < keys[j].KeyID })
	return json.Marshal(jwks{Keys: keys})
}

type claims struct {
	jwt.RegisteredClaims
	Type TokenType        `json:"typ"`
	Role user.AccountRole `json:"role,omitempty"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType string `json:"kty"`
	Use     string `json:"use"`
	Alg     string `json:"alg"`
	KeyID   string `json:"kid"`
	Modulus string `json:"n"`
	Exp     string `json:"e"`
}

func newJWK(key Key) jwk {
	return jwk{
		KeyType: "RSA",
		Use:     "sig",
		Alg:     jwt.SigningMethodRS256.Alg(),
		KeyID:   key.ID,
		Modulus: base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		Exp:     base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}
}

var _ session.TokenIssuer = (*Service)(nil)
var _ session.AccessTokenVerifier = (*Service)(nil)
