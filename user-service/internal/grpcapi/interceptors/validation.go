// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added Protovalidate enforcement with google.rpc.BadRequest details.
// Author review: ZI YANG - validate correctness

package interceptors

import (
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func validationInterceptor(validator protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		message, ok := req.(proto.Message)
		if !ok {
			return nil, status.Error(codes.Internal, "internal server error")
		}
		if err := validator.Validate(message); err != nil {
			return nil, validationStatus(err)
		}
		return handler(ctx, req)
	}
}

func validationStatus(err error) error {
	var validationErr *protovalidate.ValidationError
	if !errors.As(err, &validationErr) {
		return status.Error(codes.Internal, "internal server error")
	}

	detail := &errdetails.BadRequest{}
	for _, violation := range validationErr.Violations {
		field := ""
		description := "request field is invalid"
		if violation != nil && violation.Proto != nil {
			field = protovalidate.FieldPathString(violation.Proto.GetField())
			if violation.Proto.GetMessage() != "" {
				description = violation.Proto.GetMessage()
			}
		}
		detail.FieldViolations = append(detail.FieldViolations, &errdetails.BadRequest_FieldViolation{
			Field:       field,
			Description: description,
		})
	}
	grpcStatus, withDetailsErr := status.New(codes.InvalidArgument, "request validation failed").WithDetails(detail)
	if withDetailsErr != nil {
		return status.Error(codes.Internal, "internal server error")
	}
	return grpcStatus.Err()
}
