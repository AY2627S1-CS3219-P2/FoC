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

// AI-generated (edited by ZI YANG): replaces an open event-publication note
// with the current responsibilities of the account service.
// AccountService handles registration, profile updates, and account-status
// changes.
type AccountService struct {
	repository       UserRepository
	suspensionWriter SuspensionWriter
	sessions         SessionRepository
	otpStore         OTPStore
	otpDeliverer     OTPDeliverer
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
	return NewAccountServiceWithClockAndOTP(repository, suspensionWriter, sessions, suspensionTTL, time.Now, nil)
}

// NewAccountServiceWithClock constructs an account service with an injected
// clock for account-status invalidation.
func NewAccountServiceWithClock(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, now func() time.Time) *AccountService {
	return NewAccountServiceWithClockAndOTP(repository, suspensionWriter, sessions, suspensionTTL, now, nil)
}

// NewAccountServiceWithOTP constructs an account service with the recorded
// OTP Redis boundary for pending registrations.
func NewAccountServiceWithOTP(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, otpStore OTPStore) *AccountService {
	return NewAccountServiceWithClockAndOTPAndDelivery(repository, suspensionWriter, sessions, suspensionTTL, time.Now, otpStore, nil)
}

// NewAccountServiceWithClockAndOTP constructs an account service with injected
// clock and OTP storage dependencies.
func NewAccountServiceWithClockAndOTP(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, now func() time.Time, otpStore OTPStore) *AccountService {
	return NewAccountServiceWithClockAndOTPAndDelivery(repository, suspensionWriter, sessions, suspensionTTL, now, otpStore, nil)
}

// NewAccountServiceWithOTPAndDelivery constructs an account service with the
// recorded OTP storage and delivery boundaries.
func NewAccountServiceWithOTPAndDelivery(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, otpStore OTPStore, otpDeliverer OTPDeliverer) *AccountService {
	return NewAccountServiceWithClockAndOTPAndDelivery(repository, suspensionWriter, sessions, suspensionTTL, time.Now, otpStore, otpDeliverer)
}

// NewAccountServiceWithClockAndOTPAndDelivery constructs an account service
// with injected clock, OTP storage, and OTP delivery dependencies.
func NewAccountServiceWithClockAndOTPAndDelivery(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, now func() time.Time, otpStore OTPStore, otpDeliverer OTPDeliverer) *AccountService {
	if now == nil {
		now = time.Now
	}
	return &AccountService{
		repository:       repository,
		suspensionWriter: suspensionWriter,
		sessions:         sessions,
		otpStore:         otpStore,
		otpDeliverer:     otpDeliverer,
		suspensionTTL:    suspensionTTL,
		now:              now,
	}
}

// Register creates an unverified student account with a bcrypt password hash.
func (s *AccountService) Register(ctx context.Context, email, username, password string) (*User, error) {
	if s.otpStore != nil && s.otpDeliverer != nil {
		account, otp, err := s.RegisterWithOTP(ctx, email, username, password)
		if err != nil {
			return nil, err
		}
		if err := s.otpDeliverer.DeliverOTP(ctx, account.Email, otp); err != nil {
			return nil, fmt.Errorf("deliver registration OTP: %w", err)
		}
		return account, nil
	}
	account, err := s.newRegistrationAccount(email, username, password)
	if err != nil {
		return nil, err
	}
	if s.repository == nil {
		return nil, errors.New("user repository is required")
	}
	if err := s.repository.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	return account, nil
}

// ResendOTP replaces an unverified account's OTP only after confirming the
// account exists and has not already been activated.
func (s *AccountService) ResendOTP(ctx context.Context, email string) error {
	if s.repository == nil {
		return errors.New("user repository is required")
	}
	if s.otpStore == nil || s.otpDeliverer == nil {
		return errors.New("OTP dependencies are required")
	}
	email, err := normalizeAndValidateNUSEmail(email)
	if err != nil {
		return err
	}
	account, err := s.repository.GetByIdentifier(ctx, email)
	if err != nil {
		return fmt.Errorf("get account for OTP resend: %w", err)
	}
	if account == nil {
		return ErrNotFound
	}
	if account.AccountStatus == AccountStatusActive {
		return ErrAccountAlreadyVerified
	}
	count, err := s.otpStore.IncrementRequestCount(ctx, email)
	if err != nil {
		return fmt.Errorf("increment OTP request count: %w", err)
	}
	if OTPRequestLimitExceeded(count) {
		return ErrOTPRequestLimit
	}
	otp, err := GenerateOTP()
	if err != nil {
		return err
	}
	if err := s.otpStore.StoreHash(ctx, email, HashOTP(otp)); err != nil {
		return fmt.Errorf("store replacement OTP hash: %w", err)
	}
	if err := s.otpDeliverer.DeliverOTP(ctx, email, otp); err != nil {
		return fmt.Errorf("deliver replacement OTP: %w", err)
	}
	return nil
}

// VerifyOTP activates an account only after its supplied OTP matches Redis.
func (s *AccountService) VerifyOTP(ctx context.Context, email, otp string) error {
	if s.repository == nil || s.otpStore == nil {
		return errors.New("OTP verification dependencies are required")
	}
	email, err := normalizeAndValidateNUSEmail(email)
	if err != nil {
		return err
	}
	count, found, err := s.otpStore.RequestCount(ctx, email)
	if err != nil {
		return fmt.Errorf("get OTP request count: %w", err)
	}
	if found && OTPRequestLimitExceeded(count) {
		return ErrOTPRequestLimit
	}
	shaHash, found, err := s.otpStore.LookupHash(ctx, email)
	if err != nil {
		return fmt.Errorf("get OTP hash: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	if shaHash != HashOTP(otp) {
		return ErrInvalidOTP
	}
	if err := s.repository.ActivateUser(ctx, email); err != nil {
		return fmt.Errorf("activate verified account: %w", err)
	}
	_ = s.otpStore.Delete(ctx, email)
	return nil
}

// RegisterWithOTP creates an unverified account only when its OTP hash is
// stored successfully inside the recorded CreateWithTask boundary.
func (s *AccountService) RegisterWithOTP(ctx context.Context, email, username, password string) (*User, string, error) {
	if s.repository == nil {
		return nil, "", errors.New("user repository is required")
	}
	if s.otpStore == nil {
		return nil, "", errors.New("OTP store is required")
	}
	account, err := s.newRegistrationAccount(email, username, password)
	if err != nil {
		return nil, "", err
	}
	count, err := s.otpStore.IncrementRequestCount(ctx, account.Email)
	if err != nil {
		return nil, "", fmt.Errorf("increment OTP request count: %w", err)
	}
	if OTPRequestLimitExceeded(count) {
		return nil, "", ErrOTPRequestLimit
	}
	otp, err := GenerateOTP()
	if err != nil {
		return nil, "", err
	}
	if err := s.repository.CreateWithTask(ctx, account, func() error {
		return s.otpStore.StoreHash(ctx, account.Email, HashOTP(otp))
	}); err != nil {
		return nil, "", fmt.Errorf("create OTP-pending account: %w", err)
	}
	return account, otp, nil
}

func (s *AccountService) newRegistrationAccount(email, username, password string) (*User, error) {
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
		Email:        email,
		Username:     username,
		PasswordHash: passwordHash,
		AccountRole:  AccountRoleStudent,
		// AI Assistance Disclosure: Codex (GPT-5), 2026-10-09 — new accounts
		// remain gated until the recorded OTP flow activates them. Author review: Verify Correctness.
		AccountStatus: AccountStatusUnverified,
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

// UpdateProfile changes the username, phone number, and password. An empty
// value leaves that field unchanged; a password change verifies the current
// password first.
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

// UpdateAccountStatus sets an account ACTIVE or SUSPENDED. Suspending first
// writes the suspension key and revokes every refresh session, then saves the
// status; if either earlier step fails, the status is left unchanged.
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
