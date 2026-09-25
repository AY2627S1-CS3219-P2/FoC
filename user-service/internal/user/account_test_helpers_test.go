// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Kept account-service tests independent after authentication tests moved to the session package.
// Author review: Validated tests reflects intended behaviour

package user

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type fakeAccountRepository struct {
	user      *User
	lookupErr error
	updated   *User
	status    AccountStatus
	callOrder *[]string
	statusErr error
}

func (f *fakeAccountRepository) Create(_ context.Context, account *User) error {
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
func (f *fakeAccountRepository) UpdateAccountStatusByID(_ context.Context, _ uuid.UUID, status AccountStatus, _ time.Time) error {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "postgres")
	}
	f.status = status
	return f.statusErr
}

type fakeSuspensionWriter struct {
	err       error
	uid       uuid.UUID
	at        time.Time
	ttl       time.Duration
	callOrder *[]string
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
