// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented recorded registration, profile update, and account-status application logic.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	logger           *slog.Logger
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
	return NewAccountServiceWithClockAndOTPAndDelivery(repository, suspensionWriter, sessions, suspensionTTL, time.Now, otpStore, nil, nil)
}

// NewAccountServiceWithClockAndOTP constructs an account service with injected
// clock and OTP storage dependencies.
func NewAccountServiceWithClockAndOTP(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, now func() time.Time, otpStore OTPStore) *AccountService {
	return NewAccountServiceWithClockAndOTPAndDelivery(repository, suspensionWriter, sessions, suspensionTTL, now, otpStore, nil, nil)
}

// NewAccountServiceWithOTPAndDelivery constructs an account service with the
// recorded OTP storage and delivery boundaries. A nil logger discards the
// service's logs.
func NewAccountServiceWithOTPAndDelivery(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, otpStore OTPStore, otpDeliverer OTPDeliverer, logger *slog.Logger) *AccountService {
	return NewAccountServiceWithClockAndOTPAndDelivery(repository, suspensionWriter, sessions, suspensionTTL, time.Now, otpStore, otpDeliverer, logger)
}

// NewAccountServiceWithClockAndOTPAndDelivery constructs an account service
// with injected clock, OTP storage, and OTP delivery dependencies. A nil
// logger discards the service's logs.
func NewAccountServiceWithClockAndOTPAndDelivery(repository UserRepository, suspensionWriter SuspensionWriter, sessions SessionRepository, suspensionTTL time.Duration, now func() time.Time, otpStore OTPStore, otpDeliverer OTPDeliverer, logger *slog.Logger) *AccountService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &AccountService{
		repository:       repository,
		suspensionWriter: suspensionWriter,
		sessions:         sessions,
		otpStore:         otpStore,
		otpDeliverer:     otpDeliverer,
		suspensionTTL:    suspensionTTL,
		now:              now,
		logger:           logger,
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
// account exists and is still awaiting verification.
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
	// AI Assistance Disclosure: Codex (GPT-5), date: 2026-10-19 — issue #31
	// rejects OTP resends for every account not awaiting verification.
	if account.AccountStatus != AccountStatusUnverified {
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
	// AI-generated (edited by James Teo): the guess is counted before the stored
	// code is read and compared, so concurrent guesses cannot compare one code
	// more than OTPAttemptLimit times. The attempt after the limit discards the
	// code; the request count stays, so this does not reset the resend limit.
	attempts, err := s.otpStore.IncrementAttemptCount(ctx, email)
	if err != nil {
		return fmt.Errorf("increment OTP attempt count: %w", err)
	}
	if OTPAttemptLimitExceeded(attempts) {
		if err := s.otpStore.DeleteHash(ctx, email); err != nil {
			return fmt.Errorf("discard OTP after too many attempts: %w", err)
		}
		return ErrOTPAttemptsExceeded
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
	return s.updateProfile(ctx, uid, username, phone, newPassword, nil, func(account *User) error {
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
	return s.updateProfile(ctx, uid, username, phone, newPassword, nil, func(*User) error { return nil })
}

func (s *AccountService) updateProfile(ctx context.Context, uid uuid.UUID, username, phone, newPassword string, ac *AuditContext, authorizePasswordChange func(*User) error) (*User, error) {
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
	// Only the names of the fields that changed go to the audit log, never
	// their values: a phone number is personal data.
	var changed []string
	if username != "" {
		if username != account.Username {
			changed = append(changed, "username")
		}
		account.Username = username
	}
	if phone != "" {
		if phone != account.PhoneNum {
			changed = append(changed, "phone_num")
		}
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
		// AI-generated (edited by nigeltzy): whole seconds, see tokenCutoff.
		account.TokensValidAfter = s.tokenCutoff()
		// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements
		// user-service/AGENTS.md's password-change session invalidation using
		// the existing Redis cutoff readers. Author review: ZI YANG - reviewed and verified.
		// Publish the cutoff before persistence, as suspension does: readers
		// cannot see the PostgreSQL boundary. Both self-service changes and
		// admin resets must invalidate access and refresh tokens first.
		if s.suspensionWriter == nil || s.sessions == nil {
			return nil, errors.New("password-change invalidation dependencies are required")
		}
		if s.suspensionTTL <= 0 {
			return nil, errors.New("password-change invalidation TTL must be positive")
		}
		if err := s.suspensionWriter.WriteSuspension(ctx, uid, account.TokensValidAfter, s.suspensionTTL); err != nil {
			return nil, fmt.Errorf("write password-change invalidation: %w", err)
		}
		if err := s.sessions.RevokeAllUserSessions(ctx, uid); err != nil {
			return nil, fmt.Errorf("revoke password-changed account sessions: %w", err)
		}
	}
	if ac == nil {
		if err := s.repository.Update(ctx, account); err != nil {
			return nil, fmt.Errorf("update account: %w", err)
		}
	} else if err := s.repository.UpdateAsAdminAudited(ctx, account, profileAuditEntries(*ac, uid, changed, passwordChanged)); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	return account, nil
}

// UpdateAccountStatus sets an account ACTIVE or SUSPENDED. Suspending first
// writes the suspension key and revokes every refresh session, then saves the
// status; if either earlier step fails, the status is left unchanged. An
// UNVERIFIED account is refused with ErrAccountUnverified before any of that.
func (s *AccountService) UpdateAccountStatus(ctx context.Context, uid uuid.UUID, status AccountStatus) error {
	return s.setAccountStatus(ctx, uid, status, nil)
}

// setAccountStatus is UpdateAccountStatus; ac is nil for the unaudited call and
// the admin's identity for AdminUpdateAccountStatus, which also refuses
// suspending an admin or oneself (D-118, D-119) before any side effect.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
func (s *AccountService) setAccountStatus(ctx context.Context, uid uuid.UUID, status AccountStatus, ac *AuditContext) error {
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
	// AI Assistance Disclosure: Codex (GPT-5), 2026-10-19 — enforces D-078
	// before any admin or suspension side effect can run.
	if account.AccountStatus == AccountStatusUnverified {
		return ErrAccountUnverified
	}
	var tokensValidAfter time.Time
	var invalidate func() error
	if status == AccountStatusSuspended {
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — checks the
		// recorded last-active-admin guard before invalidating any sessions.
		// Author review: ZI YANG - validate correctness.
		if ac != nil && account.AccountRole == AccountRoleAdmin {
			if uid.String() == ac.ActorID {
				return ErrCannotSuspendSelf
			}
			return ErrCannotSuspendAdmin
		}
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
		// AI-generated (edited by nigeltzy): whole seconds, see tokenCutoff.
		tokensValidAfter = s.tokenCutoff()
		if s.suspensionWriter == nil || s.sessions == nil {
			return errors.New("suspension invalidation dependencies are required")
		}
		if s.suspensionTTL <= 0 {
			return errors.New("suspension TTL must be positive")
		}
		// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — defer side
		// effects until the audited status transaction has locked and rechecked
		// the target's role. Author review: ZI YANG - reviewed and verified.
		invalidate = func() error {
			if err := s.suspensionWriter.WriteSuspension(ctx, uid, tokensValidAfter, s.suspensionTTL); err != nil {
				return fmt.Errorf("write suspension invalidation: %w", err)
			}
			return nil
		}
	}
	if ac == nil {
		if invalidate != nil {
			if err := invalidate(); err != nil {
				return err
			}
		}
		if status == AccountStatusSuspended {
			if err := s.sessions.RevokeAllUserSessions(ctx, uid); err != nil {
				return fmt.Errorf("revoke suspended account sessions: %w", err)
			}
		}
		if err := s.repository.UpdateAccountStatusByID(ctx, uid, status, tokensValidAfter); err != nil {
			return fmt.Errorf("update account status: %w", err)
		}
		return nil
	}
	action := ActionReactivate
	if status == AccountStatusSuspended {
		action = ActionSuspend
	}
	e := ac.entry(action, uid.String(), OutcomeSucceeded, "")
	if err := s.repository.UpdateAccountStatusAudited(ctx, uid, status, tokensValidAfter, e, invalidate); err != nil {
		return fmt.Errorf("update account status: %w", err)
	}
	return nil
}

// AI-generated (edited by nigeltzy).
// tokenCutoff returns the tokens_valid_after value for an invalidation made
// now, truncated to whole seconds. A JWT's iat is whole seconds. Readers with
// no session reject a token whose iat is at or before this cutoff; refresh
// keeps its separate before rule because it also checks the refresh session,
// which is revoked by the same invalidation.
func (s *AccountService) tokenCutoff() time.Time {
	return s.now().UTC().Truncate(time.Second)
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
