// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. In-process (bufconn) gRPC server tests: round-trip
//   CRUD, admin-gate rejection, and error-code mapping.
//   2026-09-30: added the update and duplicate-rejection tests the README
//   already claimed, and a reflection-toggle test, responding to a
//   Copilot review on PR #8.
// Author review: PENDING — <reviewer to complete>

package grpcapi

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	supplierv1 "foc/supplier-service/internal/gen/supplier/v1"
	"foc/supplier-service/internal/supplier"
)

// fakeRepository is a minimal in-memory supplier.Repository, scoped to
// this package's own tests (mirrors the one in internal/supplier's own
// tests, which is unexported there).
type fakeRepository struct {
	byID       map[string]*supplier.Supplier
	duplicates map[string]bool
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{byID: map[string]*supplier.Supplier{}, duplicates: map[string]bool{}}
}

func (f *fakeRepository) Create(_ context.Context, s *supplier.Supplier) error {
	f.byID[s.ID] = s
	return nil
}
func (f *fakeRepository) GetByID(_ context.Context, id string) (*supplier.Supplier, error) {
	return f.byID[id], nil
}
func (f *fakeRepository) List(_ context.Context, _ supplier.ListFilter) ([]*supplier.Supplier, error) {
	var out []*supplier.Supplier
	for _, s := range f.byID {
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeRepository) Update(_ context.Context, s *supplier.Supplier) error {
	f.byID[s.ID] = s
	return nil
}
func (f *fakeRepository) SoftDelete(_ context.Context, id string) error {
	delete(f.byID, id)
	return nil
}
func (f *fakeRepository) ExistsDuplicate(_ context.Context, name, building, locationDescription, _ string) (bool, error) {
	return f.duplicates[name+"|"+building+"|"+locationDescription], nil
}
func (f *fakeRepository) Count(_ context.Context) (int, error) { return len(f.byID), nil }

// dial starts NewGRPCServer against an in-memory listener (bufconn — no
// TCP port, no Docker) and returns a connected client plus a cleanup func.
func dial(t *testing.T, svc *supplier.Service, enableReflection bool) supplierv1.SupplierServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := NewGRPCServer(svc, enableReflection)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return supplierv1.NewSupplierServiceClient(conn)
}

func adminCtx() context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-user-role", "ADMIN"))
}

func validWrite() *supplierv1.SupplierWrite {
	return &supplierv1.SupplierWrite{
		Name: "Cool Spot", Type: "Food", Building: "Com2",
		LocationDescription: "Opp LT16", Latitude: 1.29, Longitude: 103.77,
		IsAvailable: true,
	}
}

func TestCreateGetList_RoundTrip(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)

	created, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})
	require.NoError(t, err)
	require.NotEmpty(t, created.GetSupplier().GetId())

	got, err := client.GetSupplier(context.Background(), &supplierv1.GetSupplierRequest{Id: created.GetSupplier().GetId()})
	require.NoError(t, err)
	require.Equal(t, "Cool Spot", got.GetSupplier().GetName())

	listed, err := client.ListSuppliers(context.Background(), &supplierv1.ListSuppliersRequest{})
	require.NoError(t, err)
	require.Len(t, listed.GetSuppliers(), 1)
}

func TestCreateSupplier_WithoutAdminRole_PermissionDenied(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)

	_, err := client.CreateSupplier(context.Background(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestGetSupplier_NotFound(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)

	_, err := client.GetSupplier(context.Background(), &supplierv1.GetSupplierRequest{Id: "missing"})

	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreateSupplier_MissingRequiredField_InvalidArgument(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)
	write := validWrite()
	write.Name = ""

	_, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: write})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteSupplier_ThenGet_NotFound(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)
	created, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})
	require.NoError(t, err)

	_, err = client.DeleteSupplier(adminCtx(), &supplierv1.DeleteSupplierRequest{Id: created.GetSupplier().GetId()})
	require.NoError(t, err)

	_, err = client.GetSupplier(context.Background(), &supplierv1.GetSupplierRequest{Id: created.GetSupplier().GetId()})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateSupplier_RoundTrip(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)
	created, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})
	require.NoError(t, err)

	write := validWrite()
	write.Name = "Cool Spot Renamed"
	write.IsAvailable = false
	updated, err := client.UpdateSupplier(adminCtx(), &supplierv1.UpdateSupplierRequest{
		Id: created.GetSupplier().GetId(), Supplier: write,
	})
	require.NoError(t, err)
	require.Equal(t, "Cool Spot Renamed", updated.GetSupplier().GetName())
	require.False(t, updated.GetSupplier().GetIsAvailable())

	got, err := client.GetSupplier(context.Background(), &supplierv1.GetSupplierRequest{Id: created.GetSupplier().GetId()})
	require.NoError(t, err)
	require.Equal(t, "Cool Spot Renamed", got.GetSupplier().GetName())
}

func TestUpdateSupplier_WithoutAdminRole_PermissionDenied(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()), true)
	created, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})
	require.NoError(t, err)

	_, err = client.UpdateSupplier(context.Background(), &supplierv1.UpdateSupplierRequest{
		Id: created.GetSupplier().GetId(), Supplier: validWrite(),
	})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

// CreateSupplier must reject a duplicate name+location combination at the
// gRPC layer (FR F2.2.2) with AlreadyExists — the README claims this test
// exists; it did not until this Copilot review found the gap.
func TestCreateSupplier_Duplicate_AlreadyExists(t *testing.T) {
	fake := newFakeRepository()
	fake.duplicates["Cool Spot|Com2|Opp LT16"] = true
	client := dial(t, supplier.NewService(fake), true)

	_, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})

	require.Error(t, err)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

// TestReflection_Toggle proves the fix for the Copilot finding that
// reflection was registered unconditionally: with enableReflection=false,
// grpc.reflection.v1.ServerReflectionInfo is not among the registered
// services.
func TestReflection_Toggle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lis := bufconn.Listen(1024 * 1024)
			srv := NewGRPCServer(supplier.NewService(newFakeRepository()), tc.enabled)
			go func() { _ = srv.Serve(lis) }()
			t.Cleanup(srv.Stop)

			_, hasReflection := srv.GetServiceInfo()["grpc.reflection.v1.ServerReflection"]
			require.Equal(t, tc.enabled, hasReflection)
		})
	}
}
