// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added in-process gRPC behavior tests for RefreshSession and Logout.
// Author review: ZI YANG - validate correctness

package grpcapi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi"
	"foc/user-service/internal/grpcapi/interceptors"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type refresherStub struct {
	calls int
	token string
	pair  session.TokenPair
	err   error
}

func (s *refresherStub) Refresh(_ context.Context, token string) (session.TokenPair, error) {
	s.calls++
	s.token = token
	return s.pair, s.err
}

type accessVerifierStub struct {
	calls  int
	raw    string
	claims session.AccessTokenClaims
	err    error
}

func (s *accessVerifierStub) VerifyAccess(_ context.Context, raw string) (session.AccessTokenClaims, error) {
	s.calls++
	s.raw = raw
	return s.claims, s.err
}

type logoutterStub struct {
	calls        int
	claims       session.AccessTokenClaims
	refreshToken string
	err          error
}

func (s *logoutterStub) Logout(_ context.Context, claims session.AccessTokenClaims, refreshToken string) error {
	s.calls++
	s.claims = claims
	s.refreshToken = refreshToken
	return s.err
}

func TestServerRefreshSessionRemainsUnauthenticated(t *testing.T) {
	refresher := &refresherStub{pair: session.TokenPair{AccessToken: "new-access", RefreshToken: "new-refresh"}}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{Refresher: refresher}))

	response, err := h.client.RefreshSession(context.Background(), &userv1.RefreshSessionRequest{RefreshToken: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetAccessToken() != "new-access" || response.GetRefreshToken() != "new-refresh" {
		t.Fatalf("response = %#v", response)
	}
	if refresher.calls != 1 || refresher.token != "old-refresh" {
		t.Fatalf("refresh call = (%d, %q)", refresher.calls, refresher.token)
	}
}

func TestServerRefreshSessionValidationPrecedesService(t *testing.T) {
	refresher := &refresherStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{Refresher: refresher}))
	_, err := h.client.RefreshSession(context.Background(), &userv1.RefreshSessionRequest{})
	// AI-assisted (Codex GPT-5, 2026-09-29; review: validated correctness): retain the missing
	// refresh-token field detail from the legacy boundary.
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.InvalidArgument || !hasBadRequestField(grpcStatus, "refresh_token") {
		t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.InvalidArgument, err)
	}
	if refresher.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0", refresher.calls)
	}
}

func TestServerRefreshSessionMapsDomainFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantReason string
	}{
		{name: "invalid expired or revoked token", err: user.ErrSessionNotFound, wantCode: codes.Unauthenticated},
		{name: "suspended account", err: user.ErrAccountSuspended, wantCode: codes.Unauthenticated},
		{name: "rotated token replay", err: user.ErrSessionCompromised, wantCode: codes.Unauthenticated, wantReason: "ErrSessionCompromised"},
		{name: "refresh lock held", err: user.ErrRefreshInProgress, wantCode: codes.ResourceExhausted},
		{name: "redis or database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected failure", err: errors.New("secret refresh failure"), wantCode: codes.Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{Refresher: &refresherStub{err: test.err}}))
			_, err := h.client.RefreshSession(context.Background(), &userv1.RefreshSessionRequest{RefreshToken: "refresh-token"})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode || errorInfoReason(grpcStatus) != test.wantReason {
				t.Fatalf("status = (%s, %q), want (%s, %q); error = %v", grpcStatus.Code(), errorInfoReason(grpcStatus), test.wantCode, test.wantReason, err)
			}
		})
	}
}

func TestServerLogoutVerifiesBearerAndDelegatesWithoutIdentityMetadata(t *testing.T) {
	claims := session.AccessTokenClaims{UserID: uuid.New(), JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
	verifier := &accessVerifierStub{claims: claims}
	logoutter := &logoutterStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{AccessVerifier: verifier, Logoutter: logoutter}))

	response, err := h.client.Logout(authorizationContext("Bearer access-token"), &userv1.LogoutRequest{RefreshToken: "refresh-token"})
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("Logout response is nil")
	}
	if verifier.calls != 1 || verifier.raw != "access-token" {
		t.Fatalf("verification = (%d, %q), want (1, access-token)", verifier.calls, verifier.raw)
	}
	if logoutter.calls != 1 || logoutter.claims != claims || logoutter.refreshToken != "refresh-token" {
		t.Fatalf("logout = (%d, %#v, %q)", logoutter.calls, logoutter.claims, logoutter.refreshToken)
	}
}

func TestServerLogoutPreservesRepeatedSuccess(t *testing.T) {
	claims := session.AccessTokenClaims{UserID: uuid.New(), JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
	verifier := &accessVerifierStub{claims: claims}
	logoutter := &logoutterStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{AccessVerifier: verifier, Logoutter: logoutter}))
	ctx := authorizationContext("Bearer access-token")
	request := &userv1.LogoutRequest{RefreshToken: "refresh-token"}

	for call := 1; call <= 2; call++ {
		if _, err := h.client.Logout(ctx, request); err != nil {
			t.Fatalf("logout call %d: %v", call, err)
		}
	}
	if verifier.calls != 2 || logoutter.calls != 2 {
		t.Fatalf("calls = (%d, %d), want (2, 2)", verifier.calls, logoutter.calls)
	}
}

func TestServerLogoutRejectsMissingMalformedOrInvalidAccessToken(t *testing.T) {
	claims := session.AccessTokenClaims{UserID: uuid.New(), JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
	for name, ctx := range map[string]context.Context{
		"missing":   context.Background(),
		"malformed": authorizationContext("Basic access-token"),
	} {
		t.Run(name, func(t *testing.T) {
			verifier := &accessVerifierStub{claims: claims}
			logoutter := &logoutterStub{}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{AccessVerifier: verifier, Logoutter: logoutter}))
			_, err := h.client.Logout(ctx, &userv1.LogoutRequest{RefreshToken: "refresh-token"})
			if status.Code(err) != codes.Unauthenticated || verifier.calls != 0 || logoutter.calls != 0 {
				t.Fatalf("error/calls = %v/%d/%d", err, verifier.calls, logoutter.calls)
			}
		})
	}

	verifier := &accessVerifierStub{err: errors.New("invalid JWT")}
	logoutter := &logoutterStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{AccessVerifier: verifier, Logoutter: logoutter}))
	_, err := h.client.Logout(authorizationContext("Bearer invalid"), &userv1.LogoutRequest{RefreshToken: "refresh-token"})
	if status.Code(err) != codes.Unauthenticated || verifier.calls != 1 || logoutter.calls != 0 {
		t.Fatalf("error/calls = %v/%d/%d", err, verifier.calls, logoutter.calls)
	}
}

func TestServerLogoutValidationPrecedesTokenVerification(t *testing.T) {
	verifier := &accessVerifierStub{}
	logoutter := &logoutterStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{AccessVerifier: verifier, Logoutter: logoutter}))
	_, err := h.client.Logout(authorizationContext("Bearer access-token"), &userv1.LogoutRequest{})
	// AI-assisted (Codex GPT-5, 2026-09-29; review: validated correctness): retain the missing
	// refresh-token field detail from the legacy boundary.
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.InvalidArgument || !hasBadRequestField(grpcStatus, "refresh_token") || verifier.calls != 0 || logoutter.calls != 0 {
		t.Fatalf("error/calls = %v/%d/%d", err, verifier.calls, logoutter.calls)
	}
}

func TestServerLogoutMapsInvalidationFailures(t *testing.T) {
	claims := session.AccessTokenClaims{UserID: uuid.New(), JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "session ownership mismatch", err: session.ErrSessionOwnershipMismatch, wantCode: codes.InvalidArgument},
		{name: "redis or database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected failure", err: errors.New("secret logout failure"), wantCode: codes.Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{
				AccessVerifier: &accessVerifierStub{claims: claims},
				Logoutter:      &logoutterStub{err: test.err},
			}))
			_, err := h.client.Logout(authorizationContext("Bearer access-token"), &userv1.LogoutRequest{RefreshToken: "refresh-token"})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode {
				t.Fatalf("code = %s, want %s; error = %v", status.Code(err), test.wantCode, err)
			}
			// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — preserves the
			// legacy mismatched-session field attribution. Author review: validated correctness.
			if test.err == session.ErrSessionOwnershipMismatch && !hasBadRequestField(grpcStatus, "refresh_token") {
				t.Fatalf("details = %#v, want refresh_token violation", grpcStatus.Details())
			}
		})
	}
}

func TestServerSessionRPCsReportMissingDependencies(t *testing.T) {
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{}))
	_, refreshErr := h.client.RefreshSession(context.Background(), &userv1.RefreshSessionRequest{RefreshToken: "refresh-token"})
	_, logoutErr := h.client.Logout(authorizationContext("Bearer access-token"), &userv1.LogoutRequest{RefreshToken: "refresh-token"})
	if status.Code(refreshErr) != codes.Internal || status.Code(logoutErr) != codes.Internal {
		t.Fatalf("codes = (%s, %s), want Internal", status.Code(refreshErr), status.Code(logoutErr))
	}
}

func authorizationContext(value string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs(interceptors.AuthorizationMetadataKey, value))
}
