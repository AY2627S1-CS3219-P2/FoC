// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. Domain error -> grpc/codes mapping, transcoding the
//   former internal/httpapi/handlers.go's handleServiceError.
// Author review: PENDING — <reviewer to complete>

package grpcapi

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"foc/supplier-service/internal/supplier"
)

// toStatus maps a domain error onto the gRPC status code with the closest
// HTTP-status meaning, mirroring the former httpapi.handleServiceError:
// ErrNotFound -> 404, ErrValidation -> 400, ErrDuplicate -> 409, else 500.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, supplier.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, supplier.ErrValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, supplier.ErrDuplicate):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
