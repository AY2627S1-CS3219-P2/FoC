// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented recorded registration, profile update, and account-status application logic.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"foc/user-service/internal/hash"
	"github.com/google/uuid"
)

// AccountService owns user-facing account operations above the repository
// boundary. Event publication remains outside this service until its contract
// is recorded.
type AccountService struct {
	repository UserRepository
}

// NewAccountService constructs an account service with its repository ready for
// use.
func NewAccountService(repository UserRepository) *AccountService {
	return &AccountService{repository: repository}
}

// Register creates an active student account with a bcrypt password hash.
func (s *AccountService) Register(ctx context.Context, email, username, password string) (*User, error) {
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	if err := ValidatePassword(password); err != nil {
		return nil, fmt.Errorf("validate registration password: %w", err)
	}
	passwordHash, err := hash.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash registration password: %w", err)
	}
	account := &User{
		Email:         normalizeEmail(email),
		Username:      username,
		PasswordHash:  passwordHash,
		AccountRole:   AccountRoleStudent,
		AccountStatus: AccountStatusActive,
	}
	if err := s.repository.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	return account, nil
}

// UpdateProfile changes the recorded mutable profile fields. An empty password
// leaves the existing password unchanged.
func (s *AccountService) UpdateProfile(ctx context.Context, uid uuid.UUID, username, phone, password string) (*User, error) {
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	account, err := s.repository.GetByID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("get account for update: %w", err)
	}
	if account == nil {
		return nil, ErrNotFound
	}
	if username != "" {
		account.Username = username
	}
	if phone != "" {
		account.PhoneNum = phone
	}
	if password != "" {
		if err := ValidatePassword(password); err != nil {
			return nil, fmt.Errorf("validate profile password: %w", err)
		}
		account.PasswordHash, err = hash.HashPassword(password)
		if err != nil {
			return nil, fmt.Errorf("hash profile password: %w", err)
		}
	}
	if err := s.repository.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	return account, nil
}

// UpdateAccountStatus applies the recorded active/suspended account transition.
func (s *AccountService) UpdateAccountStatus(ctx context.Context, uid uuid.UUID, status AccountStatus, tokensValidAfter time.Time) error {
	if s.repository == nil {
		return errors.New("user repository is required")
	}
	if status != AccountStatusActive && status != AccountStatusSuspended {
		return fmt.Errorf("invalid account status %q", status)
	}
	if status == AccountStatusSuspended && tokensValidAfter.IsZero() {
		return errors.New("suspension timestamp is required")
	}
	if err := s.repository.UpdateAccountStatusByID(ctx, uid, status, tokensValidAfter); err != nil {
		return fmt.Errorf("update account status: %w", err)
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
