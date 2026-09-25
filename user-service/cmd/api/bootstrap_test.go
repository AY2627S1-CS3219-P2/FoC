// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Added focused tests for the recorded initial ADMIN bootstrap workflow.
// Author review: Verified tests reflect intended requirements

package main

import (
	"context"
	"errors"
	"testing"

	"foc/user-service/internal/hash"
	"foc/user-service/internal/user"
)

type fakeAdminBootstrapRepository struct {
	hasAdmin  bool
	checkErr  error
	createErr error
	created   *user.User
}

func (f *fakeAdminBootstrapRepository) HasAdmin(context.Context) (bool, error) {
	return f.hasAdmin, f.checkErr
}

func (f *fakeAdminBootstrapRepository) Create(_ context.Context, account *user.User) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = account
	return nil
}

func TestBootstrapInitialAdminCreatesSecureAdminWhenNoneExists(t *testing.T) {
	repository := &fakeAdminBootstrapRepository{}

	err := bootstrapInitialAdmin(context.Background(), repository, " Admin@U.NUS.EDU ", "initialadmin", "StrongAdmin1")
	if err != nil {
		t.Fatal(err)
	}
	if repository.created == nil {
		t.Fatal("initial admin was not created")
	}
	if repository.created.Email != "admin@u.nus.edu" || repository.created.AccountRole != user.AccountRoleAdmin || repository.created.AccountStatus != user.AccountStatusActive {
		t.Fatalf("created account = %#v", repository.created)
	}
	if repository.created.PasswordHash == "StrongAdmin1" || !hash.CheckPassword(repository.created.PasswordHash, "StrongAdmin1") {
		t.Fatal("initial admin password was not bcrypt-hashed")
	}
}

func TestBootstrapInitialAdminSkipsCreationWhenAdminExists(t *testing.T) {
	repository := &fakeAdminBootstrapRepository{hasAdmin: true}

	if err := bootstrapInitialAdmin(context.Background(), repository, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if repository.created != nil {
		t.Fatal("existing admin should prevent creation")
	}
}

func TestBootstrapInitialAdminRejectsIncompleteOrInvalidCredentials(t *testing.T) {
	tests := map[string]struct {
		email    string
		username string
		password string
	}{
		"missing email":    {username: "initialadmin", password: "StrongAdmin1"},
		"missing username": {email: "admin@u.nus.edu", password: "StrongAdmin1"},
		"missing password": {email: "admin@u.nus.edu", username: "initial-admin"},
		"invalid username": {email: "admin@u.nus.edu", username: "initial_admin", password: "StrongAdmin1"},
		"invalid password": {email: "admin@u.nus.edu", username: "initialadmin", password: "weakpass"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &fakeAdminBootstrapRepository{}
			if err := bootstrapInitialAdmin(context.Background(), repository, tt.email, tt.username, tt.password); err == nil {
				t.Fatal("invalid bootstrap credentials were accepted")
			}
			if repository.created != nil {
				t.Fatal("invalid bootstrap credentials were persisted")
			}
		})
	}
}

func TestBootstrapInitialAdminPropagatesRepositoryFailures(t *testing.T) {
	want := errors.New("database unavailable")
	repository := &fakeAdminBootstrapRepository{checkErr: want}

	if err := bootstrapInitialAdmin(context.Background(), repository, "admin@u.nus.edu", "initialadmin", "StrongAdmin1"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}

	repository = &fakeAdminBootstrapRepository{createErr: want}
	if err := bootstrapInitialAdmin(context.Background(), repository, "admin@u.nus.edu", "initialadmin", "StrongAdmin1"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
