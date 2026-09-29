// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added request-ID creation, response propagation, and context injection.
// Author review: ZI YANG - validate correctness

package interceptors

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const RequestIDMetadataKey = "x-request-id"

func requestIDInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		requestID := uuid.NewString()
		ctx = context.WithValue(ctx, requestIDContextKey{}, requestID)
		if err := grpc.SetHeader(ctx, metadata.Pairs(RequestIDMetadataKey, requestID)); err != nil {
			return nil, status.Error(codes.Internal, "internal server error")
		}
		return handler(ctx, req)
	}
}
