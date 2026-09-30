// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the domain authentication boundary and JWT token interface.
// Author review: Repackaged and validated correctness

package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"foc/user-service/internal/hash"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

// AI-generated (edited by ZI YANG): adds missing exported documentation and
// keeps the issuer interface focused on its observable responsibility.
// TokenPair is the access and refresh token returned to the client, plus the
// refresh token's JTI and expiry for its session record.
type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	RefreshJTI       uuid.UUID
	RefreshExpiresAt time.Time
}

// TokenIssuer creates JWT session credentials for an authenticated account.
type TokenIssuer interface {
	Issue(ctx context.Context, account *user.User) (TokenPair, error)
}

// Authenticator verifies account credentials and delegates JWT issuance.
type Authenticator struct {
	repository user.UserRepository
	issuer     TokenIssuer
}

const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJ1dL17l3Wy"

// NewAuthenticator constructs an authenticator with its persistence and JWT
// dependencies ready for use.
func NewAuthenticator(repository user.UserRepository, issuer TokenIssuer) *Authenticator {
	return &Authenticator{repository: repository, issuer: issuer}
}

// Authenticate verifies an identifier and plaintext password, then issues JWT
// access and refresh tokens for an active account.
func (a *Authenticator) Authenticate(ctx context.Context, identifier, password string) (TokenPair, error) {
	account, err := a.authenticate(ctx, identifier, password)
	if err != nil {
		return TokenPair{}, err
	}

	pair, err := a.issuer.Issue(ctx, account)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue JWT session: %w", err)
	}
	return pair, nil
}

// authenticate verifies credentials and returns the active account for flows
// that need its identity in addition to issued credentials.
func (a *Authenticator) authenticate(ctx context.Context, identifier, password string) (*user.User, error) {
	if strings.Contains(identifier, "@") {
		identifier = normalizeEmail(identifier)
	}
	account, err := a.repository.GetByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			// AI-generated (edited by ZI YANG): equalize unknown-account and wrong-password bcrypt work.
			_ = hash.CheckPassword(dummyPasswordHash, password)
			return nil, user.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("look up credentials: %w", err)
	}
	if account == nil || !hash.CheckPassword(account.PasswordHash, password) {
		return nil, user.ErrInvalidCredentials
	}
	if account.AccountStatus == user.AccountStatusSuspended {
		return nil, user.ErrAccountSuspended
	}
	return account, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
