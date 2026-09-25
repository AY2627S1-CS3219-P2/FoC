// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Implemented the recorded initial ADMIN bootstrap workflow.
// Author review: Verified correctness

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"foc/user-service/internal/hash"
	"foc/user-service/internal/user"
)

type adminBootstrapRepository interface {
	HasAdmin(ctx context.Context) (bool, error)
	Create(ctx context.Context, account *user.User) error
}

// bootstrapInitialAdmin creates the configured administrator only when the
// database does not already contain an administrator account.
func bootstrapInitialAdmin(ctx context.Context, repository adminBootstrapRepository, email, username, password string) error {
	if repository == nil {
		return errors.New("admin bootstrap repository is required")
	}
	hasAdmin, err := repository.HasAdmin(ctx)
	if err != nil {
		return fmt.Errorf("check for existing admin: %w", err)
	}
	if hasAdmin {
		return nil
	}
	if strings.TrimSpace(email) == "" || strings.TrimSpace(username) == "" || password == "" {
		return errors.New("initial admin credentials are required")
	}
	if err := user.ValidateUsername(username); err != nil {
		return fmt.Errorf("validate initial admin username: %w", err)
	}
	if err := user.ValidatePassword(password); err != nil {
		return fmt.Errorf("validate initial admin password: %w", err)
	}

	passwordHash, err := hash.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash initial admin password: %w", err)
	}
	account := &user.User{
		Email:         strings.ToLower(strings.TrimSpace(email)),
		Username:      username,
		PasswordHash:  passwordHash,
		AccountRole:   user.AccountRoleAdmin,
		AccountStatus: user.AccountStatusActive,
	}
	if err := repository.Create(ctx, account); err != nil {
		return fmt.Errorf("create initial admin: %w", err)
	}
	return nil
}
