// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: Rewritten from the HTTP-header version (>half the file changed):
//   RoleExtractor and HeaderRoleExtractor became MetadataRoleExtractor for
//   gRPC metadata, and a RequireAdmin unary interceptor replaced the
//   former http.Handler middleware chain.
//   2026-09-28: comments and behavior brought in line with D-038 (trust the
//   gateway-injected role); a missing or repeated value now counts as no role.
// Author review: PENDING — <reviewer to complete>

// Package middleware provides request-level guards for the Supplier
// Service's admin-only RPCs (FR F2.2).
//
// Identity model (D-038, which applies D-022 and D-030 to gRPC): the API
// Gateway verifies the user's access token, discards any identity the client
// sent, and injects the verified role as the "x-user-role" metadata key — the
// gRPC form of D-022's X-User-Role header. This service never sees the token
// and trusts that value. The role itself originates in user-service
// (users.account_role, put in the token's "role" claim at login).
//
// That trust is sound only while both of D-022's conditions hold: the gateway
// strips client-supplied identity, and nothing but the gateway can reach this
// service. Neither is true yet — see supplier-service/AGENTS.md.
package middleware

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const AdminRole = "ADMIN"

// RoleExtractor pulls the caller's role out of an incoming RPC context.
type RoleExtractor func(ctx context.Context) string

// MetadataRoleExtractor returns the gateway-injected "x-user-role" value. It
// returns "" — meaning no role — when the key is absent or appears more than
// once: the gateway injects exactly one value, so a repeated key means a copy
// got through from somewhere else, and this service does not pick between
// them.
func MetadataRoleExtractor(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("x-user-role")
	if len(vals) != 1 {
		return ""
	}
	return vals[0]
}

// RequireAdmin returns a unary interceptor that rejects any call to a
// method in adminMethods (full gRPC method names, e.g.
// "/foc.supplier.v1.SupplierService/CreateSupplier") whose role (per
// extract) isn't ADMIN. Calls to methods not in the set pass through
// unchecked — the equivalent of the REST router's public route.Group.
func RequireAdmin(extract RoleExtractor, adminMethods map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if adminMethods[info.FullMethod] && extract(ctx) != AdminRole {
			return nil, status.Error(codes.PermissionDenied, "admin role required")
		}
		return handler(ctx, req)
	}
}
