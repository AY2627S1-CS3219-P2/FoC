// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented the generated administrative UpdateStatus RPC adapter.
// Author review: ZI YANG - validated correctness

package grpcapi

import (
	"context"
	"errors"
	"fmt"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi/interceptors"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UpdateStatus applies an administrator-authorized account-status transition.
func (s *Server) UpdateStatus(ctx context.Context, request *userv1.UpdateStatusRequest) (*userv1.UpdateStatusResponse, error) {
	principal, ok := interceptors.PrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "verified identity metadata is required")
	}
	if principal.Role != userv1.AccountRole_ACCOUNT_ROLE_ADMIN {
		return nil, status.Error(codes.PermissionDenied, "administrator access is required")
	}
	uid, err := uuid.Parse(request.GetUid())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "uid is malformed")
	}
	accountStatus, err := domainAccountStatus(request.GetStatus())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "account status is invalid")
	}
	if s.statusUpdater == nil {
		return nil, errors.New("account status updater is required")
	}
	if err := s.statusUpdater.UpdateAccountStatus(ctx, uid, accountStatus); err != nil {
		return nil, fmt.Errorf("update account status: %w", err)
	}
	return &userv1.UpdateStatusResponse{}, nil
}

func domainAccountStatus(accountStatus userv1.AccountStatus) (user.AccountStatus, error) {
	switch accountStatus {
	case userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE:
		return user.AccountStatusActive, nil
	case userv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED:
		return user.AccountStatusSuspended, nil
	default:
		return "", fmt.Errorf("unsupported account status %q", accountStatus)
	}
}
