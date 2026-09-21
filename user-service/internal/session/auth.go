// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the domain authentication boundary and JWT token interface.
// Author review: Repackaged and validated correctness

package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/user-service/internal/hash"
	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	RefreshJTI       uuid.UUID
	RefreshExpiresAt time.Time
}

// TokenIssuer creates JWT session credentials for an authenticated account.
// The concrete signing and claims policy belongs outside the domain service.
type TokenIssuer interface {
	Issue(ctx context.Context, account *user.User) (TokenPair, error)
}

// Authenticator verifies account credentials and delegates JWT issuance.
type Authenticator struct {
	repository user.UserRepository
	issuer     TokenIssuer
}

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
	account, err := a.repository.GetByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
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
