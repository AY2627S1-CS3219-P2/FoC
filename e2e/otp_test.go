//go:build e2e

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-19
// Scope: Added the issue #31 whole-stack check that suspended accounts cannot
//   receive another OTP through the public resend route.
// Author review: ZI YANG - reviewed and verified correctness

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSuspendedAccountGetsNoNewCode verifies that a suspended student's public
// resend call is refused without adding another OTP to user-service's log.
func TestSuspendedAccountGetsNoNewCode(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student, admin := s.newStudent(t), s.admin(t)
	email := student.username + "@u.nus.edu"

	before, err := verificationCodes(t.Context(), email)
	require.NoError(t, err)
	require.Len(t, before, 1, "registration should have logged exactly one OTP")

	s.suspend(t, admin, student)
	r := s.send(t, http.MethodPost, "/auth/resend-otp", "", map[string]string{"email": email})
	expectStatus(t, r, http.StatusConflict)

	var violation string
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	poll(ctx, func(ctx context.Context) (bool, string) {
		codes, err := verificationCodes(ctx, email)
		if err != nil {
			return false, err.Error()
		}
		if len(codes) > len(before) {
			violation = fmt.Sprintf("suspended account logged %d OTPs, want %d", len(codes), len(before))
			return true, violation
		}
		return false, "no additional OTP logged"
	})
	require.Empty(t, violation)
}
