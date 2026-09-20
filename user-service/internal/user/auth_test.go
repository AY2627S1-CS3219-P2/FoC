// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added unit tests for the user authentication service boundary.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeAuthRepository struct {
	user      *User
	lookupErr error
	updated   *User
	status    AccountStatus
}

func (f *fakeAuthRepository) Create(_ context.Context, account *User) error {
	f.user = account
	return nil
}

func (f *fakeAuthRepository) GetByID(context.Context, uuid.UUID) (*User, error) {
	return f.user, f.lookupErr
}

func (f *fakeAuthRepository) GetByIdentifier(context.Context, string) (*User, error) {
	return f.user, f.lookupErr
}

func (f *fakeAuthRepository) Update(_ context.Context, got *User) error {
	f.updated = got
	return nil
}

func (f *fakeAuthRepository) UpdateAccountStatusByID(_ context.Context, _ uuid.UUID, status AccountStatus, _ time.Time) error {
	f.status = status
	return nil
}

type fakeTokenIssuer struct {
	pair      TokenPair
	err       error
	issuedFor uuid.UUID
}

func (f *fakeTokenIssuer) Issue(_ context.Context, account *User) (TokenPair, error) {
	f.issuedFor = account.UID
	return f.pair, f.err
}

func TestAuthenticatorAuthenticate(t *testing.T) {
	passwordHash, err := HashPassword("ValidPass1")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	accountID := uuid.New()
	issuerFailure := errors.New("issuer unavailable")
	tests := map[string]struct {
		accountStatus AccountStatus
		password      string
		lookupErr     error
		issuerErr     error
		wantErr       error
	}{
		"issues tokens for valid active credentials": {
			accountStatus: AccountStatusActive,
			password:      "ValidPass1",
		},
		"rejects unknown account with generic credentials error": {
			lookupErr: ErrNotFound,
			password:  "ValidPass1",
			wantErr:   ErrInvalidCredentials,
		},
		"rejects incorrect password with generic credentials error": {
			accountStatus: AccountStatusActive,
			password:      "WrongPass1",
			wantErr:       ErrInvalidCredentials,
		},
		"rejects suspended account": {
			accountStatus: AccountStatusSuspended,
			password:      "ValidPass1",
			wantErr:       ErrAccountSuspended,
		},
		"returns token issuer error": {
			accountStatus: AccountStatusActive,
			password:      "ValidPass1",
			issuerErr:     issuerFailure,
			wantErr:       issuerFailure,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			account := &User{
				UID:           accountID,
				PasswordHash:  passwordHash,
				AccountStatus: tt.accountStatus,
			}
			repo := &fakeAuthRepository{user: account, lookupErr: tt.lookupErr}
			issuer := &fakeTokenIssuer{
				pair: TokenPair{AccessToken: "access", RefreshToken: "refresh"},
				err:  tt.issuerErr,
			}
			authenticator := NewAuthenticator(repo, issuer)

			got, err := authenticator.Authenticate(context.Background(), "user@example.com", tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got != (TokenPair{}) {
					t.Fatalf("tokens = %#v, want empty pair", got)
				}
				return
			}
			if got.AccessToken != "access" || got.RefreshToken != "refresh" {
				t.Fatalf("tokens = %#v, want fake token pair", got)
			}
			if issuer.issuedFor != accountID {
				t.Fatalf("issuer user ID = %v, want %v", issuer.issuedFor, accountID)
			}
		})
	}
}
