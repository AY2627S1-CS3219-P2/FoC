// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Login and refresh reread locked role, reject changed state and discard
// credentials when persistence cannot commit.
// Author review: ZI YANG - reviewed and verified

package session

import (
	"context"
	"errors"
	"testing"
	"time"

	. "foc/user-service/internal/user"
)

func TestLoginUsesRoleReadInsideCredentialBoundary(t *testing.T) {
	account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	account.AccountRole = AccountRoleAdmin
	fresh := *account
	fresh.AccountRole = AccountRoleStudent
	sessions.beforeCredentials = func() { sessions.account = &fresh }
	svc := NewLoginService(NewAuthenticator(repo, issuer), sessions, func() time.Time { return now }, nil)
	if _, err := svc.Login(context.Background(), "student", "ValidPass1"); err != nil {
		t.Fatal(err)
	}
	if issuer.issuedRole != AccountRoleStudent {
		t.Fatalf("issued stale role %s", issuer.issuedRole)
	}
}
func TestLoginDiscardsCredentialsOnBoundaryFailure(t *testing.T) {
	_, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	sessions.credentialCommitErr = errors.New("commit failed")
	svc := NewLoginService(NewAuthenticator(repo, issuer), sessions, func() time.Time { return now }, nil)
	pair, err := svc.Login(context.Background(), "student", "ValidPass1")
	if err == nil || pair.AccessToken != "" || pair.RefreshToken != "" {
		t.Fatalf("leaked credentials on failure: %+v %v", pair, err)
	}
}
func TestRefreshValidatesAccountStateUnderCredentialLock(t *testing.T) {
	for _, status := range []AccountStatus{AccountStatusSuspended, AccountStatusUnverified} {
		t.Run(string(status), func(t *testing.T) {
			account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
			old := "old-token"
			sessions.sessions[HashRefreshToken(old)] = &Session{UID: account.UID, JTI: issuer.pair.RefreshJTI, ExpiresAt: now.Add(time.Hour)}
			sessions.beforeCredentials = func() { fresh := *account; fresh.AccountStatus = status; sessions.account = &fresh }
			verifier := &fakeRefreshVerifier{claims: RefreshClaims{UserID: account.UID, JTI: issuer.pair.RefreshJTI, IssuedAt: now}}
			svc := newRefreshService(repo, sessions, verifier, issuer, &fakeRefreshLock{acquired: true}, func() time.Time { return now })
			pair, err := svc.Refresh(context.Background(), old)
			if !errors.Is(err, ErrAccountSuspended) || pair.AccessToken != "" || sessions.rotatedOldHash != "" {
				t.Fatalf("stale state accepted: %+v %v", pair, err)
			}
		})
	}
}

func TestRefreshUsesRoleReadInsideCredentialBoundary(t *testing.T) {
	account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	account.AccountRole = AccountRoleAdmin
	fresh := *account
	fresh.AccountRole = AccountRoleStudent
	sessions.beforeCredentials = func() { sessions.account = &fresh }
	old := "old-role-token"
	sessions.sessions[HashRefreshToken(old)] = &Session{UID: account.UID, JTI: issuer.pair.RefreshJTI, ExpiresAt: now.Add(time.Hour)}
	verifier := &fakeRefreshVerifier{claims: RefreshClaims{UserID: account.UID, JTI: issuer.pair.RefreshJTI, IssuedAt: now}}
	svc := newRefreshService(repo, sessions, verifier, issuer, &fakeRefreshLock{acquired: true}, func() time.Time { return now })
	if _, err := svc.Refresh(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if issuer.issuedRole != AccountRoleStudent {
		t.Fatalf("refresh issued stale role %s", issuer.issuedRole)
	}
}

func TestLoginRejectsStateChangedBeforeCredentialLock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*User)
		want   error
	}{
		{"suspended", func(u *User) { u.AccountStatus = AccountStatusSuspended }, ErrAccountSuspended},
		{"unverified", func(u *User) { u.AccountStatus = AccountStatusUnverified }, ErrAccountUnverified},
		{"password changed", func(u *User) { u.PasswordHash = "new password hash" }, ErrInvalidCredentials},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
			fresh := *account
			tc.change(&fresh)
			sessions.beforeCredentials = func() { sessions.account = &fresh }
			svc := NewLoginService(NewAuthenticator(repo, issuer), sessions, func() time.Time { return now }, nil)
			pair, err := svc.Login(context.Background(), "student", "ValidPass1")
			if !errors.Is(err, tc.want) || pair.AccessToken != "" || sessions.created != nil {
				t.Fatalf("changed state accepted: %+v %v", pair, err)
			}
		})
	}
}
