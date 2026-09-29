// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Implemented the recorded initial ADMIN bootstrap workflow.
// Author review: Verified correctness

package main

import (
	"context"
	"errors"
	"fmt"
)

type initialAdminBootstrapper interface {
	BootstrapInitialAdmin(ctx context.Context, email, username, password string) error
}

// bootstrapInitialAdmin creates the configured administrator only when the
// database does not already contain an administrator account.
func bootstrapInitialAdmin(ctx context.Context, service initialAdminBootstrapper, email, username, password string) error {
	if service == nil {
		return errors.New("initial admin bootstrap service is required")
	}
	if err := service.BootstrapInitialAdmin(ctx, email, username, password); err != nil {
		return fmt.Errorf("bootstrap initial admin: %w", err)
	}
	return nil
}
