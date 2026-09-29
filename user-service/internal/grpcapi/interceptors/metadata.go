// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added RPC-specific extraction and validation of gateway authentication metadata.
// Author review: ZI YANG - validated correctness

package interceptors

import (
	"context"
	"strings"

	"foc/user-service/internal/gen/user/v1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// UserIDMetadataKey carries the gateway-verified account UUID.
	UserIDMetadataKey = "x-user-id"
	// UserRoleMetadataKey carries the gateway-verified account role.
	UserRoleMetadataKey = "x-user-role"
	// AuthorizationMetadataKey carries the raw access-token Bearer value.
	AuthorizationMetadataKey = "authorization"
)

func metadataInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		incoming, _ := metadata.FromIncomingContext(ctx)
		principal, err := principalFromMetadata(incoming)
		if err != nil {
			return nil, err
		}
		if requiresIdentity(info.FullMethod) && (principal.UserID == uuid.Nil || principal.Role == userv1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED) {
			return nil, status.Error(codes.Unauthenticated, "verified identity metadata is required")
		}
		if info.FullMethod == userv1.UserService_Logout_FullMethodName && !validBearer(principal.Authorization) {
			return nil, status.Error(codes.Unauthenticated, "authorization metadata is required")
		}
		return handler(context.WithValue(ctx, principalContextKey{}, principal), req)
	}
}

func principalFromMetadata(incoming metadata.MD) (Principal, error) {
	var principal Principal
	userID, err := oneMetadataValue(incoming, UserIDMetadataKey)
	if err != nil {
		return Principal{}, err
	}
	if userID != "" {
		principal.UserID, err = uuid.Parse(userID)
		if err != nil {
			return Principal{}, status.Error(codes.Unauthenticated, "x-user-id metadata is malformed")
		}
	}

	role, err := oneMetadataValue(incoming, UserRoleMetadataKey)
	if err != nil {
		return Principal{}, err
	}
	switch role {
	case "":
	case "STUDENT":
		principal.Role = userv1.AccountRole_ACCOUNT_ROLE_STUDENT
	case "ADMIN":
		principal.Role = userv1.AccountRole_ACCOUNT_ROLE_ADMIN
	default:
		return Principal{}, status.Error(codes.Unauthenticated, "x-user-role metadata is malformed")
	}

	principal.Authorization, err = oneMetadataValue(incoming, AuthorizationMetadataKey)
	if err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func oneMetadataValue(incoming metadata.MD, key string) (string, error) {
	values := incoming.Get(key)
	if len(values) > 1 {
		return "", status.Errorf(codes.Unauthenticated, "%s metadata must occur once", key)
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

func requiresIdentity(fullMethod string) bool {
	switch fullMethod {
	case userv1.UserService_GetProfile_FullMethodName,
		userv1.UserService_UpdateProfile_FullMethodName,
		userv1.UserService_UpdateStatus_FullMethodName:
		return true
	default:
		return false
	}
}

func validBearer(value string) bool {
	const prefix = "Bearer "
	return strings.HasPrefix(value, prefix) && strings.TrimSpace(strings.TrimPrefix(value, prefix)) != ""
}
