//go:build e2e

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-19
// Scope: Added whole-stack coverage for user-service protected-route
//   revocation after suspension and logout.
// Author review: ZI YANG - verified correctness

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSuspendedStudentIsRefusedAtOnce(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student, admin := s.newStudent(t), s.admin(t)
	profilePath := usersPath + "/" + student.id

	expectStatus(t, s.send(t, http.MethodGet, profilePath, student.token, nil), http.StatusOK)
	s.suspend(t, admin, student)
	expectStatus(t, s.send(t, http.MethodGet, profilePath, student.token, nil), http.StatusUnauthorized)

	r := s.send(t, http.MethodPatch, profilePath+"/status", admin.token, statusRequest{Status: "ACTIVE"})
	expectStatus(t, r, http.StatusOK)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var reactivated user
	lastErr := ""
	_, ok := poll(ctx, func(ctx context.Context) (bool, string) {
		var err error
		reactivated, err = s.login(ctx, student.username, studentPassword)
		if err != nil {
			lastErr = err.Error()
			return false, lastErr
		}
		profile := s.send(t, http.MethodGet, profilePath, reactivated.token, nil)
		if profile.status != http.StatusOK {
			lastErr = profile.String()
			return false, lastErr
		}
		return true, ""
	})
	require.True(t, ok, lastErr)
}

func TestLoggedOutTokenIsRefused(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	student := s.newStudent(t)
	profilePath := usersPath + "/" + student.id
	expectStatus(t, s.send(t, http.MethodGet, profilePath, student.token, nil), http.StatusOK)

	require.NotEmpty(t, student.refresh)
	header := http.Header{}
	header.Set("Cookie", (&http.Cookie{Name: refreshCookie, Value: student.refresh}).String())
	r, err := s.request(t.Context(), http.MethodPost, "/auth/logout", student.token, struct{}{}, header)
	require.NoError(t, err)
	expectStatus(t, r, http.StatusNoContent)
	expectStatus(t, s.send(t, http.MethodGet, profilePath, student.token, nil), http.StatusUnauthorized)
}
