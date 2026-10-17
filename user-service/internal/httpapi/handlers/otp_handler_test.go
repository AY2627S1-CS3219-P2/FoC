// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-10
// Scope: Added black-box transport tests for OTP resend and verification outcomes.
// Author review: ZI YANG - validated correctness.

package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/user"
)

type fakeOTPService struct{ err error }

func (f fakeOTPService) ResendOTP(context.Context, string) error         { return f.err }
func (f fakeOTPService) VerifyOTP(context.Context, string, string) error { return f.err }

func TestOTPHandlersMapRecordedErrors(t *testing.T) {
	for _, tt := range []struct {
		path, body string
		err        error
		want       int
	}{
		{"/api/v1/users/resend-otp", `{"email":"x@u.nus.edu"}`, user.ErrNotFound, 404},
		{"/api/v1/users/resend-otp", `{"email":"x@u.nus.edu"}`, user.ErrAccountAlreadyVerified, 409},
		{"/api/v1/users/verify-otp", `{"email":"x@u.nus.edu","otp":"123456"}`, user.ErrInvalidOTP, 400},
		{"/api/v1/users/verify-otp", `{"email":"x@u.nus.edu","otp":"123456"}`, user.ErrOTPRequestLimit, 429},
		{"/api/v1/users/verify-otp", `{"email":"x@u.nus.edu","otp":"123456"}`, user.ErrOTPAttemptsExceeded, 429}, // AI-generated (edited by James Teo).
	} {
		t.Run(tt.path, func(t *testing.T) {
			r := newTestRouter(routes.Dependencies{Auth: handlers.AuthDependencies{OTPResender: fakeOTPService{tt.err}, OTPVerifier: fakeOTPService{tt.err}}})
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status=%d want %d", res.Code, tt.want)
			}
		})
	}
}

// AI-generated (edited by James Teo): after too many wrong guesses the code is
// gone, so the 429 tells the user to request a new one.
func TestVerifyOTPHandlerAsksForNewOTPAfterTooManyAttempts(t *testing.T) {
	r := newTestRouter(routes.Dependencies{Auth: handlers.AuthDependencies{OTPVerifier: fakeOTPService{user.ErrOTPAttemptsExceeded}}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/verify-otp", strings.NewReader(`{"email":"x@u.nus.edu","otp":"123456"}`))
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusTooManyRequests || !strings.Contains(res.Body.String(), "request a new OTP") {
		t.Fatalf("status=%d body=%s; want 429 asking for a new OTP", res.Code, res.Body.String())
	}
}

var _ = errors.New
