// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added in-process tests for the standard gRPC health service.
// Author review: ZI YANG - validated correctness

package grpcapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestHealthServerReportsWholeServiceDependencyState(t *testing.T) {
	tests := []struct {
		name         string
		postgresErr  error
		redisErr     error
		want         healthv1.HealthCheckResponse_ServingStatus
		wantPostgres int
		wantRedis    int
	}{
		{name: "healthy", want: healthv1.HealthCheckResponse_SERVING, wantPostgres: 1, wantRedis: 1},
		{name: "postgres unavailable", postgresErr: errors.New("postgres unavailable"), want: healthv1.HealthCheckResponse_NOT_SERVING, wantPostgres: 1},
		{name: "redis unavailable", redisErr: errors.New("redis unavailable"), want: healthv1.HealthCheckResponse_NOT_SERVING, wantPostgres: 1, wantRedis: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			postgresCalls := 0
			redisCalls := 0
			healthServer, err := grpcapi.NewHealthServer(
				func(context.Context) error { postgresCalls++; return test.postgresErr },
				func(context.Context) error { redisCalls++; return test.redisErr },
			)
			if err != nil {
				t.Fatal(err)
			}
			client := newHealthClient(t, healthServer)

			response, err := client.Check(context.Background(), &healthv1.HealthCheckRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if response.GetStatus() != test.want {
				t.Fatalf("status = %s, want %s", response.GetStatus(), test.want)
			}
			if postgresCalls != test.wantPostgres || redisCalls != test.wantRedis {
				t.Fatalf("dependency calls = (%d, %d), want (%d, %d)", postgresCalls, redisCalls, test.wantPostgres, test.wantRedis)
			}
		})
	}
}

func TestHealthServerSupportsUserServiceNameAndRejectsUnknownServices(t *testing.T) {
	healthServer, err := grpcapi.NewHealthServer(
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	client := newHealthClient(t, healthServer)

	response, err := client.Check(context.Background(), &healthv1.HealthCheckRequest{Service: userv1.UserService_ServiceDesc.ServiceName})
	if err != nil || response.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("named service response/error = %v/%v", response, err)
	}

	_, err = client.Check(context.Background(), &healthv1.HealthCheckRequest{Service: "unknown.Service"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown service code = %s, want %s; error = %v", status.Code(err), codes.NotFound, err)
	}
}

func TestNewHealthServerRequiresBothDependencies(t *testing.T) {
	check := func(context.Context) error { return nil }
	if _, err := grpcapi.NewHealthServer(nil, check); err == nil {
		t.Fatal("missing PostgreSQL check was accepted")
	}
	if _, err := grpcapi.NewHealthServer(check, nil); err == nil {
		t.Fatal("missing Redis check was accepted")
	}
}

func newHealthClient(t *testing.T, healthServer healthv1.HealthServer) healthv1.HealthClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	logger := slog.New(slog.NewJSONHandler(new(bytes.Buffer), nil))
	server, err := grpcapi.NewGRPCServer(logger, &recordingService{}, healthServer)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///bufnet-health",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	connection.Connect()
	return healthv1.NewHealthClient(connection)
}
