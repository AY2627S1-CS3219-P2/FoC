// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added in-process tests for the recorded gRPC transport foundations.
// Author review: ZI YANG - validate correctness

package grpcapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi"
	"foc/user-service/internal/grpcapi/interceptors"
	"foc/user-service/internal/user"

	"buf.build/go/protovalidate"
	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type recordingService struct {
	userv1.UnimplementedUserServiceServer
	registerCalls atomic.Int32
	// AI-assisted: injects a domain failure so tests exercise the complete interceptor chain.
	registerErr error
	principal   chan interceptors.Principal
}

func (s *recordingService) Register(context.Context, *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	s.registerCalls.Add(1)
	return &userv1.RegisterResponse{}, s.registerErr
}

func (s *recordingService) Login(context.Context, *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	panic("test panic")
}

func (s *recordingService) GetProfile(ctx context.Context, _ *userv1.GetProfileRequest) (*userv1.GetProfileResponse, error) {
	principal, _ := interceptors.PrincipalFromContext(ctx)
	s.principal <- principal
	return &userv1.GetProfileResponse{Profile: &userv1.UserProfile{
		Uid:      principal.UserID.String(),
		Username: "student1",
		Email:    "student1@u.nus.edu",
	}}, nil
}

func (s *recordingService) RefreshSession(context.Context, *userv1.RefreshSessionRequest) (*userv1.RefreshSessionResponse, error) {
	return &userv1.RefreshSessionResponse{AccessToken: "access", RefreshToken: "refresh"}, nil
}

func (s *recordingService) Logout(ctx context.Context, _ *userv1.LogoutRequest) (*userv1.LogoutResponse, error) {
	principal, _ := interceptors.PrincipalFromContext(ctx)
	s.principal <- principal
	return &userv1.LogoutResponse{}, nil
}

type harness struct {
	client userv1.UserServiceClient
	logs   *bytes.Buffer
}

func newHarness(t *testing.T, service userv1.UserServiceServer) harness {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	logs := new(bytes.Buffer)
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — supplies the
	// mandatory standard health service to the test transport. Author review: validated correctness.
	healthServer, err := grpcapi.NewHealthServer(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	server, err := grpcapi.NewGRPCServer(logger, service, healthServer)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	connection.Connect()

	return harness{client: userv1.NewUserServiceClient(connection), logs: logs}
}

func TestRefreshSessionRemainsUnauthenticated(t *testing.T) {
	service := &recordingService{principal: make(chan interceptors.Principal, 1)}
	h := newHarness(t, service)

	response, err := h.client.RefreshSession(context.Background(), &userv1.RefreshSessionRequest{RefreshToken: "refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetAccessToken() != "access" || response.GetRefreshToken() != "refresh" {
		t.Fatalf("response = %#v", response)
	}
}

func TestProtectedRPCExtractsVerifiedIdentityMetadata(t *testing.T) {
	service := &recordingService{principal: make(chan interceptors.Principal, 1)}
	h := newHarness(t, service)
	uid := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		interceptors.UserIDMetadataKey, uid.String(),
		interceptors.UserRoleMetadataKey, "STUDENT",
	))

	if _, err := h.client.GetProfile(ctx, &userv1.GetProfileRequest{Uid: uid.String()}); err != nil {
		t.Fatal(err)
	}
	principal := <-service.principal
	if principal.UserID != uid || principal.Role != userv1.AccountRole_ACCOUNT_ROLE_STUDENT {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestProtectedRPCRejectsMissingOrMalformedMetadata(t *testing.T) {
	uid := uuid.NewString()
	for name, ctx := range map[string]context.Context{
		"missing": context.Background(),
		"malformed user id": metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
			interceptors.UserIDMetadataKey, "not-a-uuid",
			interceptors.UserRoleMetadataKey, "ADMIN",
		)),
		"malformed role": metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
			interceptors.UserIDMetadataKey, uid,
			interceptors.UserRoleMetadataKey, "OWNER",
		)),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, &recordingService{principal: make(chan interceptors.Principal, 1)})
			_, err := h.client.GetProfile(ctx, &userv1.GetProfileRequest{Uid: uid})
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.Unauthenticated, err)
			}
		})
	}
}

func TestLogoutExtractsAuthorizationWithoutRequiringIdentity(t *testing.T) {
	service := &recordingService{principal: make(chan interceptors.Principal, 1)}
	h := newHarness(t, service)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		interceptors.AuthorizationMetadataKey, "Bearer access-token",
	))

	if _, err := h.client.Logout(ctx, &userv1.LogoutRequest{RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	if got := (<-service.principal).Authorization; got != "Bearer access-token" {
		t.Fatalf("authorization = %q", got)
	}

	_, err := h.client.Logout(context.Background(), &userv1.LogoutRequest{RefreshToken: "refresh"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing authorization code = %s, want %s", status.Code(err), codes.Unauthenticated)
	}
}

func TestValidationReturnsBadRequestDetailsBeforeHandler(t *testing.T) {
	service := &recordingService{principal: make(chan interceptors.Principal, 1)}
	h := newHarness(t, service)

	_, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email:    "invalid",
		Username: "bad name",
		Password: "short",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.InvalidArgument, err)
	}
	if service.registerCalls.Load() != 0 {
		t.Fatalf("handler calls = %d, want 0", service.registerCalls.Load())
	}
	grpcStatus := status.Convert(err)
	var badRequest *errdetails.BadRequest
	for _, detail := range grpcStatus.Details() {
		if typed, ok := detail.(*errdetails.BadRequest); ok {
			badRequest = typed
		}
	}
	if badRequest == nil || len(badRequest.GetFieldViolations()) == 0 {
		t.Fatalf("details = %#v, want BadRequest field violations", grpcStatus.Details())
	}
}

func TestProtovalidateCompilesRecordedContract(t *testing.T) {
	err := protovalidate.Validate(&userv1.RegisterRequest{
		Email:    "student1@u.nus.edu",
		Username: "student1",
		Password: "Password1",
	})
	if err != nil {
		t.Fatalf("validate generated RegisterRequest: %v", err)
	}
}

func TestRequestIDIsReturnedAndLogged(t *testing.T) {
	h := newHarness(t, &recordingService{principal: make(chan interceptors.Principal, 1)})
	var headers metadata.MD
	_, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email:    "student1@u.nus.edu",
		Username: "student1",
		Password: "Password1",
	}, grpc.Header(&headers))
	if err != nil {
		t.Fatal(err)
	}
	requestIDs := headers.Get(interceptors.RequestIDMetadataKey)
	if len(requestIDs) != 1 {
		t.Fatalf("request ID headers = %#v", requestIDs)
	}
	if _, err := uuid.Parse(requestIDs[0]); err != nil {
		t.Fatalf("request ID = %q: %v", requestIDs[0], err)
	}
	for _, want := range []string{`"method":"/user.v1.UserService/Register"`, `"request_id":"` + requestIDs[0] + `"`, `"code":"OK"`} {
		if !strings.Contains(h.logs.String(), want) {
			t.Errorf("logs = %s, want %s", h.logs.String(), want)
		}
	}
}

func TestPanicsAreLoggedAndSanitized(t *testing.T) {
	h := newHarness(t, &recordingService{principal: make(chan interceptors.Principal, 1)})
	_, err := h.client.Login(context.Background(), &userv1.LoginRequest{Identifier: "student1", Password: "Password1"})
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "internal server error" {
		t.Fatalf("error = %v, want sanitized Internal", err)
	}
	for _, want := range []string{`"panic":"test panic"`, `"stack_trace":"`, `"request_id":"`} {
		if !strings.Contains(h.logs.String(), want) {
			t.Errorf("logs = %s, want %s", h.logs.String(), want)
		}
	}
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — verifies structured
// domain errors over the in-process gRPC transport. Author review: ZI YANG - validated correctness.
func TestDomainErrorsAreMappedThroughServer(t *testing.T) {
	h := newHarness(t, &recordingService{
		principal:   make(chan interceptors.Principal, 1),
		registerErr: user.ErrDuplicateEmail,
	})
	_, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email:    "student1@u.nus.edu",
		Username: "student1",
		Password: "Password1",
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.AlreadyExists {
		t.Fatalf("code = %s, want %s; error = %v", grpcStatus.Code(), codes.AlreadyExists, err)
	}
	var reason string
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			reason = info.GetReason()
		}
	}
	if reason != "ErrDuplicateEmail" {
		t.Fatalf("ErrorInfo reason = %q, want ErrDuplicateEmail", reason)
	}
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — verifies the raw
// failure is logged without exposing it to the caller. Author review: ZI YANG - validated correctness.
func TestUnexpectedErrorsAreLoggedAndSanitized(t *testing.T) {
	h := newHarness(t, &recordingService{
		principal:   make(chan interceptors.Principal, 1),
		registerErr: errors.New("secret database detail"),
	})
	_, err := h.client.Register(context.Background(), &userv1.RegisterRequest{
		Email:    "student1@u.nus.edu",
		Username: "student1",
		Password: "Password1",
	})
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "internal server error" {
		t.Fatalf("error = %v, want sanitized Internal", err)
	}
	if !strings.Contains(h.logs.String(), `"error":"secret database detail"`) {
		t.Fatalf("logs = %s, want raw error detail", h.logs.String())
	}
}

func TestRequestsLargerThanTenKiBAreRejected(t *testing.T) {
	h := newHarness(t, &recordingService{principal: make(chan interceptors.Principal, 1)})
	_, err := h.client.Login(context.Background(), &userv1.LoginRequest{
		Identifier: "student1",
		Password:   strings.Repeat("x", 11*1024),
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.ResourceExhausted, err)
	}
}
