// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added the generated-server foundation and configured unary gRPC transport.
// Author review: ZI YANG - validate correctness

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented the generated Register and Login RPC adapters.
// Author review: ZI YANG - validate correctness

// Package grpcapi implements the user-service gRPC transport.
package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi/interceptors"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// MaxRequestBytes is the recorded maximum inbound protobuf message size.
const MaxRequestBytes = 10 * 1024

// Registrar creates an account from registration credentials.
type Registrar interface {
	Register(context.Context, string, string, string) (*user.User, error)
}

// LoginService authenticates an account and persists its refresh session.
type LoginService interface {
	Login(context.Context, string, string) (session.TokenPair, error)
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — adds the narrow
// profile operations consumed by the generated RPC adapter. Author review: validated correctness.
// ProfileGetter retrieves an account by ID.
type ProfileGetter interface {
	GetByID(context.Context, uuid.UUID) (*user.User, error)
}

// ProfileUpdater changes the caller's own mutable profile fields.
type ProfileUpdater interface {
	UpdateProfile(context.Context, uuid.UUID, string, string, string, string) (*user.User, error)
}

// AdminProfileUpdater changes another account's mutable profile fields.
type AdminProfileUpdater interface {
	UpdateProfileAsAdmin(context.Context, uuid.UUID, string, string, string) (*user.User, error)
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — adds the recorded
// account-status domain boundary. Author review: Validated correctness.
// AccountStatusUpdater suspends or reactivates an account.
type AccountStatusUpdater interface {
	UpdateAccountStatus(context.Context, uuid.UUID, user.AccountStatus) error
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — adds the recorded
// refresh and logout application boundaries. Author review: validated correctness.
// Refresher rotates a valid refresh-token session.
type Refresher interface {
	Refresh(context.Context, string) (session.TokenPair, error)
}

// AccessTokenVerifier verifies the access credential used by Logout.
type AccessTokenVerifier interface {
	VerifyAccess(context.Context, string) (session.AccessTokenClaims, error)
}

// Logoutter invalidates a verified access token and refresh session.
type Logoutter interface {
	Logout(context.Context, session.AccessTokenClaims, string) error
}

// Dependencies contains the domain operations used by the user RPC adapter.
type Dependencies struct {
	Registrar           Registrar
	LoginService        LoginService
	ProfileGetter       ProfileGetter
	ProfileUpdater      ProfileUpdater
	AdminProfileUpdater AdminProfileUpdater
	StatusUpdater       AccountStatusUpdater
	Refresher           Refresher
	AccessVerifier      AccessTokenVerifier
	Logoutter           Logoutter
}

// Server is the user RPC adapter.
type Server struct {
	userv1.UnimplementedUserServiceServer
	registrar           Registrar
	loginService        LoginService
	profileGetter       ProfileGetter
	profileUpdater      ProfileUpdater
	adminProfileUpdater AdminProfileUpdater
	statusUpdater       AccountStatusUpdater
	refresher           Refresher
	accessVerifier      AccessTokenVerifier
	logoutter           Logoutter
}

// NewServer constructs a ready-to-register user RPC adapter.
func NewServer(deps Dependencies) *Server {
	return &Server{
		registrar:           deps.Registrar,
		loginService:        deps.LoginService,
		profileGetter:       deps.ProfileGetter,
		profileUpdater:      deps.ProfileUpdater,
		adminProfileUpdater: deps.AdminProfileUpdater,
		statusUpdater:       deps.StatusUpdater,
		refresher:           deps.Refresher,
		accessVerifier:      deps.AccessVerifier,
		logoutter:           deps.Logoutter,
	}
}

// Register creates an account using the generated request and response types.
func (s *Server) Register(ctx context.Context, request *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	if s.registrar == nil {
		return nil, errors.New("registrar is required")
	}
	if _, err := s.registrar.Register(ctx, request.GetEmail(), request.GetUsername(), request.GetPassword()); err != nil {
		return nil, fmt.Errorf("register account: %w", err)
	}
	return &userv1.RegisterResponse{}, nil
}

// Login authenticates an account and returns its issued access and refresh tokens.
func (s *Server) Login(ctx context.Context, request *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	if s.loginService == nil {
		return nil, errors.New("login service is required")
	}
	pair, err := s.loginService.Login(ctx, request.GetIdentifier(), request.GetPassword())
	if err != nil {
		return nil, fmt.Errorf("login account: %w", err)
	}
	return &userv1.LoginResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
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
