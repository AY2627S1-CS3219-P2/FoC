// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Kept account-service tests independent after authentication tests moved to the session package.
// Author review: ZI YANG - Validated tests reflects intended behaviour

package user

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type fakeAccountRepository struct {
	user                 *User
	hasAdmin             bool
	activeAdmins         int
	countActiveAdminsErr error
	lookupErr            error
	updated              *User
	status               AccountStatus
	callOrder            *[]string
	statusErr            error
	statusErrs           []error   // AI-generated (edited by ZI YANG).
	tokensValidAfter     time.Time // AI-generated (edited by nigeltzy).
	createWithTaskCalled bool
}

func (f *fakeAccountRepository) HasAdmin(context.Context) (bool, error) {
	return f.hasAdmin, f.lookupErr
}

func (f *fakeAccountRepository) CountActiveAdmins(context.Context) (int, error) {
	return f.activeAdmins, f.countActiveAdminsErr
}

func (f *fakeAccountRepository) Create(_ context.Context, account *User) error {
	f.user = account
	return nil
}
func (f *fakeAccountRepository) CreateWithTask(_ context.Context, account *User, task func() error) error {
	f.createWithTaskCalled = true
	if err := task(); err != nil {
		return err
	}
	f.user = account
	return nil
}
func (f *fakeAccountRepository) GetByID(context.Context, uuid.UUID) (*User, error) {
	return f.user, f.lookupErr
}
func (f *fakeAccountRepository) GetByIdentifier(context.Context, string) (*User, error) {
	return f.user, f.lookupErr
}
func (f *fakeAccountRepository) Update(_ context.Context, account *User) error {
	f.updated = account
	return nil
}
func (f *fakeAccountRepository) UpdateLastLoginByID(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (f *fakeAccountRepository) UpdateAccountStatusByID(_ context.Context, _ uuid.UUID, status AccountStatus, tokensValidAfter time.Time) error {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "postgres")
	}
	f.status = status
	f.tokensValidAfter = tokensValidAfter // AI-generated (edited by nigeltzy).
	// AI-generated (edited by ZI YANG).
	if len(f.statusErrs) > 0 {
		err := f.statusErrs[0]
		f.statusErrs = f.statusErrs[1:]
		return err
	}
	return f.statusErr
}
func (f *fakeAccountRepository) ActivateUser(_ context.Context, _ string) error {
	f.status = AccountStatusActive
	return nil
}

type fakeSuspensionWriter struct {
	err       error
	uid       uuid.UUID
	at        time.Time
	ttl       time.Duration
	callOrder *[]string
}

type fakeOTPStore struct {
	count        int64
	incrementErr error
	storedEmail  string
	storedHash   string
	storeErr     error
	hash         string
	hashFound    bool
	requestFound bool
	deleteErr    error
}

func (f *fakeOTPStore) LookupHash(context.Context, string) (string, bool, error) {
	return f.hash, f.hashFound, nil
}
func (f *fakeOTPStore) RequestCount(context.Context, string) (int64, bool, error) {
	return f.count, f.requestFound, nil
}
func (f *fakeOTPStore) Delete(context.Context, string) error { return f.deleteErr }

type fakeOTPDeliverer struct {
	email string
	otp   string
	err   error
}

func (f *fakeOTPDeliverer) DeliverOTP(_ context.Context, email, otp string) error {
	f.email, f.otp = email, otp
	return f.err
}

func (f *fakeOTPStore) IncrementRequestCount(_ context.Context, _ string) (int64, error) {
	return f.count, f.incrementErr
}

func (f *fakeOTPStore) StoreHash(_ context.Context, email, hash string) error {
	f.storedEmail, f.storedHash = email, hash
	return f.storeErr
}

func (f *fakeSuspensionWriter) WriteSuspension(_ context.Context, uid uuid.UUID, at time.Time, ttl time.Duration) error {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "redis")
	}
	f.uid, f.at, f.ttl = uid, at, ttl
	return f.err
}

type fakeAccountSessionRepository struct {
	err       error
	uid       uuid.UUID
	callOrder *[]string
}

func (f *fakeAccountSessionRepository) CreateSession(context.Context, *Session) error {
	return nil
}
func (f *fakeAccountSessionRepository) GetSessionByHash(context.Context, string) (*Session, error) {
	return nil, nil
}
func (f *fakeAccountSessionRepository) RotateSession(context.Context, string, *Session) error {
	return nil
}
func (f *fakeAccountSessionRepository) RevokeSessionByHash(context.Context, string) error {
	return nil
}
func (f *fakeAccountSessionRepository) RevokeAllUserSessions(_ context.Context, uid uuid.UUID) error {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "sessions")
	}
	f.uid = uid
	return f.err
}
