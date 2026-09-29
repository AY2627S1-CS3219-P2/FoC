// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added in-process gRPC behavior tests for Register and Login.
// Author review: ZI YANG - validated correctness

package grpcapi_test

import (
	"context"
	"errors"
	"testing"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type registrarStub struct {
	calls    int
	email    string
	username string
	password string
	err      error
}

func (s *registrarStub) Register(_ context.Context, email, username, password string) (*user.User, error) {
	s.calls++
	s.email = email
	s.username = username
	s.password = password
	return &user.User{}, s.err
}

type loginStub struct {
	calls      int
	identifier string
	password   string
	pair       session.TokenPair
	err        error
}

func (s *loginStub) Login(_ context.Context, identifier, password string) (session.TokenPair, error) {
	s.calls++
	s.identifier = identifier
	s.password = password
	return s.pair, s.err
}

type connectionFailure struct{}

func (connectionFailure) Error() string   { return "connection refused" }
func (connectionFailure) Timeout() bool   { return false }
func (connectionFailure) Temporary() bool { return true }

func TestServerRegisterDelegatesToAccountService(t *testing.T) {
	registrar := &registrarStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{Registrar: registrar}))

	response, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email:    "student1@u.nus.edu",
		Username: "student1",
		Password: "Password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("Register response is nil")
	}
	if registrar.calls != 1 || registrar.email != "student1@u.nus.edu" || registrar.username != "student1" || registrar.password != "Password1" {
		t.Fatalf("registrar call = (%d, %q, %q, %q)", registrar.calls, registrar.email, registrar.username, registrar.password)
	}
}

func TestServerRegisterValidationRunsBeforeDomainService(t *testing.T) {
	registrar := &registrarStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{Registrar: registrar}))

	_, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email:    "not-an-email",
		Username: "invalid username",
		Password: "short",
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.InvalidArgument {
		t.Fatalf("code = %s, want %s", grpcStatus.Code(), codes.InvalidArgument)
	}
	if registrar.calls != 0 {
		t.Fatalf("registrar calls = %d, want 0", registrar.calls)
	}
	if !hasBadRequestDetail(grpcStatus) {
		t.Fatalf("details = %#v, want BadRequest", grpcStatus.Details())
	}
}

func TestServerRegisterMapsDomainFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantReason string
	}{
		{name: "duplicate email", err: user.ErrDuplicateEmail, wantCode: codes.AlreadyExists, wantReason: "ErrDuplicateEmail"},
		{name: "duplicate username", err: user.ErrDuplicateUsername, wantCode: codes.AlreadyExists, wantReason: "ErrDuplicateUsername"},
		{name: "database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected persistence error", err: errors.New("secret query failure"), wantCode: codes.Internal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{Registrar: &registrarStub{err: test.err}}))
			_, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
				Email:    "student1@u.nus.edu",
				Username: "student1",
				Password: "Password1",
			})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode {
				t.Fatalf("code = %s, want %s; error = %v", grpcStatus.Code(), test.wantCode, err)
			}
			if reason := errorInfoReason(grpcStatus); reason != test.wantReason {
				t.Fatalf("ErrorInfo reason = %q, want %q", reason, test.wantReason)
			}
		})
	}
}

func TestServerLoginReturnsIssuedTokenPair(t *testing.T) {
	login := &loginStub{pair: session.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
	}}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{LoginService: login}))

	response, err := h.client.Login(context.Background(), &userv1.LoginRequest{
		Identifier: "Student1@U.NUS.EDU",
		Password:   "Password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetAccessToken() != "access-token" || response.GetRefreshToken() != "refresh-token" {
		t.Fatalf("response = %#v", response)
	}
	if login.calls != 1 || login.identifier != "Student1@U.NUS.EDU" || login.password != "Password1" {
		t.Fatalf("login call = (%d, %q, %q)", login.calls, login.identifier, login.password)
	}
}

func TestServerLoginValidationRunsBeforeDomainService(t *testing.T) {
	login := &loginStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{LoginService: login}))

	_, err := h.client.Login(context.Background(), &userv1.LoginRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.InvalidArgument, err)
	}
	if login.calls != 0 {
		t.Fatalf("login calls = %d, want 0", login.calls)
	}
}

func TestServerLoginMapsDomainFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantReason string
	}{
		{name: "invalid credentials", err: user.ErrInvalidCredentials, wantCode: codes.Unauthenticated},
		{name: "suspended account", err: user.ErrAccountSuspended, wantCode: codes.PermissionDenied, wantReason: "ErrAccountSuspended"},
		{name: "database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected persistence error", err: errors.New("secret query failure"), wantCode: codes.Internal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{LoginService: &loginStub{err: test.err}}))
			_, err := h.client.Login(context.Background(), &userv1.LoginRequest{
				Identifier: "student1",
				Password:   "Password1",
			})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode {
				t.Fatalf("code = %s, want %s; error = %v", grpcStatus.Code(), test.wantCode, err)
			}
			if reason := errorInfoReason(grpcStatus); reason != test.wantReason {
				t.Fatalf("ErrorInfo reason = %q, want %q", reason, test.wantReason)
			}
		})
	}
}

func TestServerMissingAuthDependenciesReturnInternal(t *testing.T) {
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{}))

	_, registerErr := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email: "student1@u.nus.edu", Username: "student1", Password: "Password1",
	})
	_, loginErr := h.client.Login(context.Background(), &userv1.LoginRequest{
		Identifier: "student1", Password: "Password1",
	})
	if status.Code(registerErr) != codes.Internal || status.Code(loginErr) != codes.Internal {
		t.Fatalf("codes = (%s, %s), want Internal", status.Code(registerErr), status.Code(loginErr))
	}
}

func hasBadRequestDetail(grpcStatus *status.Status) bool {
	for _, detail := range grpcStatus.Details() {
		if _, ok := detail.(*errdetails.BadRequest); ok {
			return true
		}
	}
	return false
}

func errorInfoReason(grpcStatus *status.Status) string {
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}
