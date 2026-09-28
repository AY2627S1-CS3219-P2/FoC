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
	now              func() time.Time
}

const (
	maxEmailCharacters = 255
	maxPhoneCharacters = 20
)

// NewAccountService constructs an account service with the dependencies
// required to invalidate suspended accounts before persistence.
func NewAccountService(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration) *AccountService {
	return NewAccountServiceWithClock(repository, suspensionWriter, sessions, suspensionTTL, time.Now)
}

// NewAccountServiceWithClock constructs an account service with an injected
// clock for account-status invalidation.
func NewAccountServiceWithClock(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, now func() time.Time) *AccountService {
	if now == nil {
		now = time.Now
	}
	return &AccountService{
		repository:       repository,
		suspensionWriter: suspensionWriter,
		sessions:         sessions,
		suspensionTTL:    suspensionTTL,
		now:              now,
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

// BootstrapInitialAdmin creates the configured administrator only when none exists.
func (s *AccountService) BootstrapInitialAdmin(ctx context.Context, email, username, password string) error {
	if s.repository == nil {
		return errors.New("user repository is required")
	}
	hasAdmin, err := s.repository.HasAdmin(ctx)
	if err != nil {
		return fmt.Errorf("check for existing admin: %w", err)
	}
	if hasAdmin {
		return nil
	}
	if strings.TrimSpace(email) == "" || strings.TrimSpace(username) == "" || password == "" {
		return errors.New("initial admin credentials are required")
	}
	email, err = normalizeAndValidateNUSEmail(email)
	if err != nil {
		return err
	}
	if err := ValidateUsername(username); err != nil {
		return fmt.Errorf("validate initial admin username: %w", err)
	}
	if err := ValidatePassword(password); err != nil {
		return fmt.Errorf("validate initial admin password: %w", err)
	}
	passwordHash, err := hash.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash initial admin password: %w", err)
	}
	// AI-generated (edited by ZI YANG): the domain owns the only bootstrap-specific administrator creation path.
	if err := s.repository.Create(ctx, &User{
		Email:         email,
		Username:      username,
		PasswordHash:  passwordHash,
		AccountRole:   AccountRoleAdmin,
		AccountStatus: AccountStatusActive,
	}); err != nil {
		return fmt.Errorf("create initial admin: %w", err)
	}
	return nil
}

// GetByID retrieves an account through the domain boundary.
func (s *AccountService) GetByID(ctx context.Context, uid uuid.UUID) (*User, error) {
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	// AI-generated (edited by ZI YANG): keep HTTP consumers independent of the persistence adapter.
	account, err := s.repository.GetByID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("get account by ID: %w", err)
	}
	return account, nil
}

// UpdateProfile changes mutable profile fields and verifies the current
// password before changing it.
func (s *AccountService) UpdateProfile(ctx context.Context, uid uuid.UUID, username, phone, currentPassword, newPassword string) (*User, error) {
	return s.updateProfile(ctx, uid, username, phone, newPassword, func(account *User) error {
		if !hash.CheckPassword(account.PasswordHash, currentPassword) {
			return ErrInvalidCurrentPassword
		}
		return nil
	})
}

// UpdateProfileAsAdmin changes mutable profile fields for another account.
// An administrator may reset the target account's password without its current
// password, as recorded for administrative recovery.
func (s *AccountService) UpdateProfileAsAdmin(ctx context.Context, uid uuid.UUID, username, phone, newPassword string) (*User, error) {
	return s.updateProfile(ctx, uid, username, phone, newPassword, func(*User) error { return nil })
}

func (s *AccountService) updateProfile(ctx context.Context, uid uuid.UUID, username, phone, newPassword string, authorizePasswordChange func(*User) error) (*User, error) {
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
	passwordChanged := newPassword != ""
	if passwordChanged {
		if err := authorizePasswordChange(account); err != nil {
			return nil, err
		}
		if err := ValidatePassword(newPassword); err != nil {
			return nil, fmt.Errorf("validate profile password: %w", err)
		}
		account.PasswordHash, err = hash.HashPassword(newPassword)
		if err != nil {
			return nil, fmt.Errorf("hash profile password: %w", err)
		}
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — applies the
		// recorded access-token validity boundary after a password reset.
		// Author review: ZI YANG - validated correctness
		account.TokensValidAfter = s.now().UTC()
	}
	if err := s.repository.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	if passwordChanged {
		if s.sessions == nil {
			return nil, errors.New("session repository is required")
		}
		if err := s.sessions.RevokeAllUserSessions(ctx, uid); err != nil {
			return nil, fmt.Errorf("revoke password-changed account sessions: %w", err)
		}
	}
	return account, nil
}

// UpdateAccountStatus applies the recorded active/suspended account transition.
func (s *AccountService) UpdateAccountStatus(ctx context.Context, uid uuid.UUID, status AccountStatus) error {
	if s.repository == nil {
		return errors.New("user repository is required")
	}
	if status != AccountStatusActive && status != AccountStatusSuspended {
		return fmt.Errorf("invalid account status %q", status)
	}
	// AI-generated (edited by ZI YANG): verify the account exists before invalidating its sessions.
	account, err := s.repository.GetByID(ctx, uid)
	if err != nil {
		return fmt.Errorf("get account for status update: %w", err)
	}
	if account == nil {
		return ErrNotFound
	}
	var tokensValidAfter time.Time
	if status == AccountStatusSuspended {
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — checks the
		// recorded last-active-admin guard before invalidating any sessions.
		// Author review: ZI YANG - validate correctness.
		if account.AccountRole == AccountRoleAdmin {
			activeAdmins, err := s.repository.CountActiveAdmins(ctx)
			if err != nil {
				return fmt.Errorf("count active admins: %w", err)
			}
			if activeAdmins == 1 {
				return ErrLastAdmin
			}
		}
		// AI-generated (edited by ZI YANG): the domain owns the timestamp shared by suspension invalidation and persistence.
		tokensValidAfter = s.now().UTC()
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
	// AI-generated (edited by ZI YANG): the domain owns canonical NUS-email validation for all registration callers.
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
