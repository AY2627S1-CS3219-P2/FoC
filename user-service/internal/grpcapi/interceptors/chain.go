// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Composed the recorded unary interceptor order with injected dependencies.
// Author review: ZI YANG - validated correctness of logic

package interceptors

import (
	"errors"
	"fmt"
	"log/slog"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
)

// NewUnaryChain builds the user-service unary interceptor chain.
func NewUnaryChain(logger *slog.Logger) ([]grpc.UnaryServerInterceptor, error) {
	if logger == nil {
		return nil, errors.New("gRPC logger is required")
	}
	validator, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("create protobuf validator: %w", err)
	}
	return []grpc.UnaryServerInterceptor{
		requestIDInterceptor(),
		loggingInterceptor(logger),
		recoveryInterceptor(logger),
		metadataInterceptor(),
		validationInterceptor(validator),
	}, nil
}
