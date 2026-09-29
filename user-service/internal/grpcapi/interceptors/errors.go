// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added the centralized domain-to-gRPC error boundary recorded by the service contract.
// Author review: ZI YANG - validated correctness

package interceptors

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"syscall"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const errorInfoDomain = "user-service"

func errorMappingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		if err == nil {
			return response, nil
		}

		mapped := mapError(info.FullMethod, err)
		if code := status.Code(mapped); code == codes.Internal || code == codes.Unavailable {
			logger.ErrorContext(ctx, "gRPC request failed",
				"request_id", RequestIDFromContext(ctx),
				"method", info.FullMethod,
				"code", code.String(),
				"error", err,
			)
		}
		return response, mapped
	}
}

func mapError(method string, err error) error {
	if err == nil {
		return nil
	}
	if grpcStatus, ok := status.FromError(err); ok && grpcStatus.Code() != codes.Unknown {
		switch grpcStatus.Code() {
		case codes.Internal:
			return status.Error(codes.Internal, "internal server error")
		case codes.Unavailable:
			return status.Error(codes.Unavailable, "service dependency unavailable")
		default:
			return err
		}
	}

	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	case errors.Is(err, user.ErrDuplicateEmail):
		return errorInfoStatus(codes.AlreadyExists, "account already exists", "ErrDuplicateEmail")
	case errors.Is(err, user.ErrDuplicateUsername):
		return errorInfoStatus(codes.AlreadyExists, "account already exists", "ErrDuplicateUsername")
	case errors.Is(err, user.ErrInvalidEmail), errors.Is(err, user.ErrEmailTooLong):
		return fieldViolationStatus("email")
	case errors.Is(err, user.ErrInvalidUsername):
		return fieldViolationStatus("username")
	case errors.Is(err, user.ErrInvalidPassword):
		field := "password"
		if method == userv1.UserService_UpdateProfile_FullMethodName {
			field = "new_password"
		}
		return fieldViolationStatus(field)
	case errors.Is(err, user.ErrInvalidPhone):
		return fieldViolationStatus("phone_num")
	case errors.Is(err, user.ErrInvalidCredentials), errors.Is(err, user.ErrInvalidCurrentPassword):
		return status.Error(codes.Unauthenticated, "authentication failed")
	case errors.Is(err, user.ErrAccountSuspended):
		if method == userv1.UserService_RefreshSession_FullMethodName {
			return status.Error(codes.Unauthenticated, "authentication failed")
		}
		return errorInfoStatus(codes.PermissionDenied, "account is suspended", "ErrAccountSuspended")
	case errors.Is(err, user.ErrNotFound):
		return status.Error(codes.NotFound, "user not found")
	case errors.Is(err, user.ErrLastAdmin):
		return errorInfoStatus(codes.FailedPrecondition, "cannot suspend the last active admin", "ErrLastAdmin")
	case errors.Is(err, user.ErrSessionNotFound):
		return status.Error(codes.Unauthenticated, "authentication failed")
	case errors.Is(err, user.ErrSessionCompromised):
		return errorInfoStatus(codes.Unauthenticated, "authentication failed", "ErrSessionCompromised")
	case errors.Is(err, user.ErrRefreshInProgress):
		return status.Error(codes.ResourceExhausted, "refresh already in progress")
	case errors.Is(err, session.ErrSessionOwnershipMismatch):
		return fieldViolationStatus("refresh_token")
	case isDependencyUnavailable(err):
		return status.Error(codes.Unavailable, "service dependency unavailable")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

func fieldViolationStatus(field string) error {
	detail := &errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{{
		Field:       field,
		Description: "request field is invalid",
	}}}
	grpcStatus, err := status.New(codes.InvalidArgument, "request field is invalid").WithDetails(detail)
	if err != nil {
		return status.Error(codes.Internal, "internal server error")
	}
	return grpcStatus.Err()
}

func errorInfoStatus(code codes.Code, message, reason string) error {
	detail := &errdetails.ErrorInfo{Reason: reason, Domain: errorInfoDomain}
	grpcStatus, err := status.New(code, message).WithDetails(detail)
	if err != nil {
		return status.Error(codes.Internal, "internal server error")
	}
	return grpcStatus.Err()
}

func isDependencyUnavailable(err error) bool {
	var networkErr net.Error
	return errors.As(err, &networkErr) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}
