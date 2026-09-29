// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented generated RefreshSession and Logout RPC adapters.
// Author review: ZI YANG - validated correctness

package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi/interceptors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RefreshSession rotates the request's refresh credential without requiring
// caller identity or authorization metadata.
func (s *Server) RefreshSession(ctx context.Context, request *userv1.RefreshSessionRequest) (*userv1.RefreshSessionResponse, error) {
	if s.refresher == nil {
		return nil, errors.New("refresh service is required")
	}
	pair, err := s.refresher.Refresh(ctx, request.GetRefreshToken())
	if err != nil {
		return nil, fmt.Errorf("refresh session: %w", err)
	}
	return &userv1.RefreshSessionResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
}

// Logout verifies the forwarded access credential and invalidates both access
// and refresh session state.
func (s *Server) Logout(ctx context.Context, request *userv1.LogoutRequest) (*userv1.LogoutResponse, error) {
	if s.accessVerifier == nil || s.logoutter == nil {
		return nil, errors.New("logout dependencies are required")
	}
	principal, ok := interceptors.PrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authorization metadata is required")
	}
	rawAccessToken, ok := bearerToken(principal.Authorization)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authorization metadata is required")
	}
	claims, err := s.accessVerifier.VerifyAccess(ctx, rawAccessToken)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, status.Error(codes.Unauthenticated, "access token is invalid")
	}
	if err := s.logoutter.Logout(ctx, claims, request.GetRefreshToken()); err != nil {
		return nil, fmt.Errorf("logout session: %w", err)
	}
	return &userv1.LogoutResponse{}, nil
}

func bearerToken(value string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}
