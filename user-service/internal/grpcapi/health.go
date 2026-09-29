// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented the standard gRPC health service over PostgreSQL and Redis checks.
// Author review: ZI YANG - validated correctness

package grpcapi

import (
	"context"
	"errors"

	userv1 "foc/user-service/internal/gen/user/v1"

	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// HealthCheck reports whether one required service dependency is reachable.
type HealthCheck func(context.Context) error

// HealthServer implements the standard gRPC health-checking protocol.
type HealthServer struct {
	healthv1.UnimplementedHealthServer
	postgresCheck HealthCheck
	redisCheck    HealthCheck
}

// NewHealthServer constructs a health service that fails closed when either
// PostgreSQL or Redis is unavailable.
func NewHealthServer(postgresCheck, redisCheck HealthCheck) (*HealthServer, error) {
	if postgresCheck == nil {
		return nil, errors.New("PostgreSQL health check is required")
	}
	if redisCheck == nil {
		return nil, errors.New("Redis health check is required")
	}
	return &HealthServer{postgresCheck: postgresCheck, redisCheck: redisCheck}, nil
}

// Check reports the aggregate health of the process or generated user service.
func (s *HealthServer) Check(ctx context.Context, request *healthv1.HealthCheckRequest) (*healthv1.HealthCheckResponse, error) {
	service := request.GetService()
	if service != "" && service != userv1.UserService_ServiceDesc.ServiceName {
		return nil, status.Errorf(codes.NotFound, "unknown service %q", service)
	}

	servingStatus := healthv1.HealthCheckResponse_SERVING
	if err := s.postgresCheck(ctx); err != nil {
		servingStatus = healthv1.HealthCheckResponse_NOT_SERVING
	} else if err := s.redisCheck(ctx); err != nil {
		servingStatus = healthv1.HealthCheckResponse_NOT_SERVING
	}
	return &healthv1.HealthCheckResponse{Status: servingStatus}, nil
}
