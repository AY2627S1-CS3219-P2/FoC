// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added the generated-server foundation and configured unary gRPC transport.
// Author review: ZI YANG - validate correctness

// Package grpcapi implements the user-service gRPC transport.
package grpcapi

import (
	"errors"
	"log/slog"

	"foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi/interceptors"

	"google.golang.org/grpc"
)

// MaxRequestBytes is the recorded maximum inbound protobuf message size.
const MaxRequestBytes = 10 * 1024

// Server is the user RPC adapter. Domain dependencies are added as individual
// RPCs are implemented.
type Server struct {
	userv1.UnimplementedUserServiceServer
}

// NewServer constructs a ready-to-register user RPC adapter.
func NewServer() *Server {
	return &Server{}
}

// NewGRPCServer constructs and registers the unary user-service transport.
func NewGRPCServer(logger *slog.Logger, implementation userv1.UserServiceServer) (*grpc.Server, error) {
	if logger == nil {
		return nil, errors.New("gRPC logger is required")
	}
	if implementation == nil {
		return nil, errors.New("user service implementation is required")
	}

	unaryInterceptors, err := interceptors.NewUnaryChain(logger)
	if err != nil {
		return nil, err
	}
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(MaxRequestBytes),
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
	)
	userv1.RegisterUserServiceServer(server, implementation)
	return server, nil
}
