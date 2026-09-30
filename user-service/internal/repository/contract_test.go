// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added compile-time checks for the recorded repository contracts.
// Author review: COMPLETED BY ZI YANG

package repository

import (
	"foc/user-service/internal/user"
)

var _ user.UserRepository = (*PostgresRepository)(nil)
var _ user.SessionRepository = (*PostgresSessionRepository)(nil)
