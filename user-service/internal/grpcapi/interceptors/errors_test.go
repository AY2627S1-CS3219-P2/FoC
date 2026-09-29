// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added table-driven tests for the recorded domain-to-gRPC error contract.
// Author review: ZI YANG - validated correctness

package interceptors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type unavailableNetworkError struct{}

func (unavailableNetworkError) Error() string   { return "connection reset by peer" }
func (unavailableNetworkError) Timeout() bool   { return false }
func (unavailableNetworkError) Temporary() bool { return true }

func TestMapErrorCoversRecordedDomainContract(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		err         error
		wantCode    codes.Code
		wantMessage string
		wantField   string
		wantReason  string
	}{
		{name: "invalid email", method: userv1.UserService_Register_FullMethodName, err: user.ErrInvalidEmail, wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "email"},
		{name: "email too long", method: userv1.UserService_Register_FullMethodName, err: fmt.Errorf("validate: %w", user.ErrEmailTooLong), wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "email"},
		{name: "invalid username", method: userv1.UserService_Register_FullMethodName, err: user.ErrInvalidUsername, wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "username"},
		{name: "invalid registration password", method: userv1.UserService_Register_FullMethodName, err: user.ErrInvalidPassword, wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "password"},
		{name: "invalid updated password", method: userv1.UserService_UpdateProfile_FullMethodName, err: user.ErrInvalidPassword, wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "new_password"},
		{name: "invalid phone", method: userv1.UserService_UpdateProfile_FullMethodName, err: user.ErrInvalidPhone, wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "phone_num"},
		{name: "duplicate email", method: userv1.UserService_Register_FullMethodName, err: user.ErrDuplicateEmail, wantCode: codes.AlreadyExists, wantMessage: "account already exists", wantReason: "ErrDuplicateEmail"},
		{name: "duplicate username", method: userv1.UserService_Register_FullMethodName, err: user.ErrDuplicateUsername, wantCode: codes.AlreadyExists, wantMessage: "account already exists", wantReason: "ErrDuplicateUsername"},
		{name: "invalid credentials", method: userv1.UserService_Login_FullMethodName, err: user.ErrInvalidCredentials, wantCode: codes.Unauthenticated, wantMessage: "authentication failed"},
		{name: "invalid current password", method: userv1.UserService_UpdateProfile_FullMethodName, err: user.ErrInvalidCurrentPassword, wantCode: codes.Unauthenticated, wantMessage: "authentication failed"},
		{name: "suspended login", method: userv1.UserService_Login_FullMethodName, err: user.ErrAccountSuspended, wantCode: codes.PermissionDenied, wantMessage: "account is suspended", wantReason: "ErrAccountSuspended"},
		{name: "suspended refresh", method: userv1.UserService_RefreshSession_FullMethodName, err: user.ErrAccountSuspended, wantCode: codes.Unauthenticated, wantMessage: "authentication failed"},
		{name: "user not found", method: userv1.UserService_GetProfile_FullMethodName, err: user.ErrNotFound, wantCode: codes.NotFound, wantMessage: "user not found"},
		{name: "last active admin", method: userv1.UserService_UpdateStatus_FullMethodName, err: user.ErrLastAdmin, wantCode: codes.FailedPrecondition, wantMessage: "cannot suspend the last active admin", wantReason: "ErrLastAdmin"},
		{name: "session not found", method: userv1.UserService_RefreshSession_FullMethodName, err: user.ErrSessionNotFound, wantCode: codes.Unauthenticated, wantMessage: "authentication failed"},
		{name: "session compromised", method: userv1.UserService_RefreshSession_FullMethodName, err: user.ErrSessionCompromised, wantCode: codes.Unauthenticated, wantMessage: "authentication failed", wantReason: "ErrSessionCompromised"},
		{name: "refresh lock held", method: userv1.UserService_RefreshSession_FullMethodName, err: user.ErrRefreshInProgress, wantCode: codes.ResourceExhausted, wantMessage: "refresh already in progress"},
		{name: "session owner mismatch", method: userv1.UserService_Logout_FullMethodName, err: session.ErrSessionOwnershipMismatch, wantCode: codes.InvalidArgument, wantMessage: "request field is invalid", wantField: "refresh_token"},
		{name: "canceled context", method: userv1.UserService_Register_FullMethodName, err: context.Canceled, wantCode: codes.Canceled, wantMessage: "request canceled"},
		{name: "expired deadline", method: userv1.UserService_Register_FullMethodName, err: context.DeadlineExceeded, wantCode: codes.DeadlineExceeded, wantMessage: "request deadline exceeded"},
		{name: "dependency connection failure", method: userv1.UserService_Register_FullMethodName, err: fmt.Errorf("insert user: %w", unavailableNetworkError{}), wantCode: codes.Unavailable, wantMessage: "service dependency unavailable"},
		{name: "unexpected failure", method: userv1.UserService_Register_FullMethodName, err: errors.New("secret query detail"), wantCode: codes.Internal, wantMessage: "internal server error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := status.Convert(mapError(test.method, fmt.Errorf("service operation: %w", test.err)))
			if got.Code() != test.wantCode || got.Message() != test.wantMessage {
				t.Fatalf("status = (%s, %q), want (%s, %q)", got.Code(), got.Message(), test.wantCode, test.wantMessage)
			}
			assertErrorDetails(t, got, test.wantField, test.wantReason)
		})
	}
}

func TestMapErrorPreservesExistingGRPCStatus(t *testing.T) {
	want := status.Error(codes.PermissionDenied, "caller is not permitted")
	if got := mapError(userv1.UserService_UpdateStatus_FullMethodName, want); got != want {
		t.Fatalf("mapError() returned %v, want original status %v", got, want)
	}
}

func assertErrorDetails(t *testing.T, grpcStatus *status.Status, wantField, wantReason string) {
	t.Helper()
	var gotField, gotReason string
	for _, detail := range grpcStatus.Details() {
		switch typed := detail.(type) {
		case *errdetails.BadRequest:
			if len(typed.GetFieldViolations()) > 0 {
				gotField = typed.GetFieldViolations()[0].GetField()
			}
		case *errdetails.ErrorInfo:
			gotReason = typed.GetReason()
		}
	}
	if gotField != wantField || gotReason != wantReason {
		t.Fatalf("details = %#v, got field %q reason %q; want field %q reason %q", grpcStatus.Details(), gotField, gotReason, wantField, wantReason)
	}
}
