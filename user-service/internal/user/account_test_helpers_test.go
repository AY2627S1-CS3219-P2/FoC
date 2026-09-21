// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Kept account-service tests independent after authentication tests moved to the session package.
// Author review: PENDING — reviewer to complete

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
	f.status = status
	return nil
}
