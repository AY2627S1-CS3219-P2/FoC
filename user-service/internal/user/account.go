// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented recorded registration, profile update, and account-status application logic.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"foc/user-service/internal/hash"

	"github.com/google/uuid"
)

// AccountService owns user-facing account operations above the repository
// boundary. Event publication remains outside this service until its contract
// is recorded.
type AccountService struct {
	repository       UserRepository
	suspensionWriter SuspensionWriter
	sessions         SessionRepository
	suspensionTTL    time.Duration
}

const (
	maxEmailCharacters = 255
	maxPhoneCharacters = 20
)

// NewAccountService constructs an account service with the dependencies
// required to invalidate suspended accounts before persistence.
func NewAccountService(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration) *AccountService {
	return &AccountService{
		repository:       repository,
		suspensionWriter: suspensionWriter,
		sessions:         sessions,
		suspensionTTL:    suspensionTTL,
	}
}

// Register creates an active student account with a bcrypt password hash.
func (s *AccountService) Register(ctx context.Context, email, username, password string) (*User, error) {
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	email, err := normalizeAndValidateNUSEmail(email)
	if err != nil {
		return nil, err
	}
	// AI-generated (edited by ZI YANG): match the recorded PostgreSQL VARCHAR(255) limit before persistence.
	if utf8.RuneCountInString(email) > maxEmailCharacters {
		return nil, ErrEmailTooLong
	}
	if err := ValidateUsername(username); err != nil {
		return nil, fmt.Errorf("validate registration username: %w", err)
	}
	if err := ValidatePassword(password); err != nil {
		return nil, fmt.Errorf("validate registration password: %w", err)
	}
	passwordHash, err := hash.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash registration password: %w", err)
	}
	account := &User{
		Email:         email,
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
	if username != "" {
		if err := ValidateUsername(username); err != nil {
			return nil, fmt.Errorf("validate profile username: %w", err)
		}
	}
	// AI-generated (edited by ZI YANG): match the recorded PostgreSQL VARCHAR(20) limit without imposing phone syntax.
	if phone != "" && utf8.RuneCountInString(phone) > maxPhoneCharacters {
		return nil, ErrInvalidPhone
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
	// AI-generated (edited by ZI YANG): verify the account exists before invalidating its sessions.
	account, err := s.repository.GetByID(ctx, uid)
	if err != nil {
		return fmt.Errorf("get account for status update: %w", err)
	}
	if account == nil {
		return ErrNotFound
	}
	if status == AccountStatusSuspended {
		if s.suspensionWriter == nil || s.sessions == nil {
			return errors.New("suspension invalidation dependencies are required")
		}
		if s.suspensionTTL <= 0 {
			return errors.New("suspension TTL must be positive")
		}
		if err := s.suspensionWriter.WriteSuspension(ctx, uid, tokensValidAfter, s.suspensionTTL); err != nil {
			return fmt.Errorf("write suspension invalidation: %w", err)
		}
		if err := s.sessions.RevokeAllUserSessions(ctx, uid); err != nil {
			return fmt.Errorf("revoke suspended account sessions: %w", err)
		}
	}
	if err := s.repository.UpdateAccountStatusByID(ctx, uid, status, tokensValidAfter); err != nil {
		return fmt.Errorf("update account status: %w", err)
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeAndValidateNUSEmail(email string) (string, error) {
	// AI-generated (edited by PENDING): the domain owns canonical NUS-email validation for all registration callers.
	email = normalizeEmail(email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", ErrInvalidEmail
	}
	at := strings.LastIndexByte(parsed.Address, '@')
	if at <= 0 || !strings.EqualFold(parsed.Address[at:], "@u.nus.edu") {
		return "", ErrInvalidEmail
	}
	return email, nil
}
