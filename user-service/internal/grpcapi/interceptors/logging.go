// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added structured completion logging for unary gRPC requests.
// Author review: ZI YANG - validated correctness

package interceptors

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func loggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		logger.InfoContext(ctx, "gRPC request completed",
			"request_id", RequestIDFromContext(ctx),
			"method", info.FullMethod,
			"code", status.Code(err).String(),
		)
		return response, err
	}
}
