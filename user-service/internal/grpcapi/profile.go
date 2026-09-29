// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented generated GetProfile and UpdateProfile RPC adapters.
// Author review: ZI YANG - reviewed correctness

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
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetProfile returns common fields to any authenticated caller and
// administrative fields only to administrators.
func (s *Server) GetProfile(ctx context.Context, request *userv1.GetProfileRequest) (*userv1.GetProfileResponse, error) {
	principal, ok := interceptors.PrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "verified identity metadata is required")
	}
	uid, err := uuid.Parse(request.GetUid())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "uid is malformed")
	}
	if s.profileGetter == nil {
		return nil, errors.New("profile getter is required")
	}
	account, err := s.profileGetter.GetByID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	if account == nil {
		return nil, user.ErrNotFound
	}

	profile, err := profileMessage(account, principal.Role == userv1.AccountRole_ACCOUNT_ROLE_ADMIN)
	if err != nil {
		return nil, err
	}
	return &userv1.GetProfileResponse{Profile: profile}, nil
}

// UpdateProfile applies a self-service update or an administrative reset and
// returns the recorded full profile response.
func (s *Server) UpdateProfile(ctx context.Context, request *userv1.UpdateProfileRequest) (*userv1.UpdateProfileResponse, error) {
	principal, ok := interceptors.PrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "verified identity metadata is required")
	}
	uid, err := uuid.Parse(request.GetUid())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "uid is malformed")
	}
	if principal.UserID != uid && principal.Role != userv1.AccountRole_ACCOUNT_ROLE_ADMIN {
		return nil, status.Error(codes.PermissionDenied, "caller cannot update this profile")
	}

	// AccountService uses empty values as its established no-change sentinel,
	// so omitted protobuf optionals are forwarded as empty without manufacturing
	// updates for fields the caller did not send.
	var account *user.User
	if principal.Role == userv1.AccountRole_ACCOUNT_ROLE_ADMIN && principal.UserID != uid {
		if s.adminProfileUpdater == nil {
			return nil, errors.New("admin profile updater is required")
		}
		account, err = s.adminProfileUpdater.UpdateProfileAsAdmin(
			ctx,
			uid,
			request.GetUsername(),
			request.GetPhoneNum(),
			request.GetNewPassword(),
		)
	} else {
		if s.profileUpdater == nil {
			return nil, errors.New("profile updater is required")
		}
		account, err = s.profileUpdater.UpdateProfile(
			ctx,
			uid,
			request.GetUsername(),
			request.GetPhoneNum(),
			request.GetCurrentPassword(),
			request.GetNewPassword(),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	if account == nil {
		return nil, user.ErrNotFound
	}

	profile, err := profileMessage(account, true)
	if err != nil {
		return nil, err
	}
	return &userv1.UpdateProfileResponse{Profile: profile}, nil
}

func profileMessage(account *user.User, includeAdministrativeFields bool) (*userv1.UserProfile, error) {
	profile := &userv1.UserProfile{
		Uid:      account.UID.String(),
		Username: account.Username,
		Email:    account.Email,
		PhoneNum: account.PhoneNum,
	}
	if !includeAdministrativeFields {
		return profile, nil
	}

	role, err := protobufAccountRole(account.AccountRole)
	if err != nil {
		return nil, err
	}
	accountStatus, err := protobufAccountStatus(account.AccountStatus)
	if err != nil {
		return nil, err
	}
	createdAt := timestamppb.New(account.DateCreated)
	if err := createdAt.CheckValid(); err != nil {
		return nil, fmt.Errorf("convert account creation time: %w", err)
	}
	profile.AccountRole = &role
	profile.AccountStatus = &accountStatus
	profile.DateCreated = createdAt
	return profile, nil
}

func protobufAccountRole(role user.AccountRole) (userv1.AccountRole, error) {
	switch role {
	case user.AccountRoleStudent:
		return userv1.AccountRole_ACCOUNT_ROLE_STUDENT, nil
	case user.AccountRoleAdmin:
		return userv1.AccountRole_ACCOUNT_ROLE_ADMIN, nil
	default:
		return userv1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, fmt.Errorf("unsupported account role %q", role)
	}
}

func protobufAccountStatus(accountStatus user.AccountStatus) (userv1.AccountStatus, error) {
	switch accountStatus {
	case user.AccountStatusActive:
		return userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, nil
	case user.AccountStatusSuspended:
		return userv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED, nil
	default:
		return userv1.AccountStatus_ACCOUNT_STATUS_UNSPECIFIED, fmt.Errorf("unsupported account status %q", accountStatus)
	}
}
