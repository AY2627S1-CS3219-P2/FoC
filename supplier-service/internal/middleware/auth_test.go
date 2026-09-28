// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. Tests for the metadata-based role extractor and the
//   admin-only unary interceptor.
// Author review: PENDING — <reviewer to complete>

package middleware

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func withRole(role string) context.Context {
	if role == "" {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-user-role", role))
}

// A client-supplied copy that slips past the gateway would sit beside the
// gateway's own value. The extractor must not choose between them.
func TestMetadataRoleExtractor_RepeatedValueIsNoRole(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("x-user-role", "STUDENT", "x-user-role", "ADMIN"))
	assert.Equal(t, "", MetadataRoleExtractor(ctx))

	reversed := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("x-user-role", "ADMIN", "x-user-role", "STUDENT"))
	assert.Equal(t, "", MetadataRoleExtractor(reversed))
}

func TestRequireAdmin_RepeatedRoleValueDeniedEvenWhenOneSaysAdmin(t *testing.T) {
	methods := map[string]bool{"/foc.supplier.v1.SupplierService/DeleteSupplier": true}
	interceptor := RequireAdmin(MetadataRoleExtractor, methods)
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("x-user-role", "ADMIN", "x-user-role", "ADMIN"))
	handlerCalled := false
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "ok", nil
	}

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/foc.supplier.v1.SupplierService/DeleteSupplier"}, handler)

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, handlerCalled)
}

func TestMetadataRoleExtractor(t *testing.T) {
	assert.Equal(t, "ADMIN", MetadataRoleExtractor(withRole("ADMIN")))
	assert.Equal(t, "", MetadataRoleExtractor(withRole("")))
	assert.Equal(t, "STUDENT", MetadataRoleExtractor(withRole("STUDENT")))
}

func TestRequireAdmin_BlocksNonAdminOnGuardedMethod(t *testing.T) {
	methods := map[string]bool{"/foc.supplier.v1.SupplierService/CreateSupplier": true}
	interceptor := RequireAdmin(MetadataRoleExtractor, methods)
	handlerCalled := false
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "ok", nil
	}

	_, err := interceptor(withRole("STUDENT"), nil, &grpc.UnaryServerInfo{FullMethod: "/foc.supplier.v1.SupplierService/CreateSupplier"}, handler)

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, handlerCalled, "handler must not run when the role check fails")
}

func TestRequireAdmin_AllowsAdminOnGuardedMethod(t *testing.T) {
	methods := map[string]bool{"/foc.supplier.v1.SupplierService/CreateSupplier": true}
	interceptor := RequireAdmin(MetadataRoleExtractor, methods)
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }

	resp, err := interceptor(withRole("ADMIN"), nil, &grpc.UnaryServerInfo{FullMethod: "/foc.supplier.v1.SupplierService/CreateSupplier"}, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
}

func TestRequireAdmin_UnguardedMethodPassesThroughWithoutRole(t *testing.T) {
	methods := map[string]bool{"/foc.supplier.v1.SupplierService/CreateSupplier": true}
	interceptor := RequireAdmin(MetadataRoleExtractor, methods)
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }

	resp, err := interceptor(withRole(""), nil, &grpc.UnaryServerInfo{FullMethod: "/foc.supplier.v1.SupplierService/GetSupplier"}, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
}
