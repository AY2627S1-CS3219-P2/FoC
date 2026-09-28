// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. In-process (bufconn) gRPC server tests: round-trip
//   CRUD, admin-gate rejection, and error-code mapping.
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
	byID map[string]*supplier.Supplier
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{byID: map[string]*supplier.Supplier{}}
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
func (f *fakeRepository) ExistsDuplicate(_ context.Context, _, _, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeRepository) Count(_ context.Context) (int, error) { return len(f.byID), nil }

// dial starts NewGRPCServer against an in-memory listener (bufconn — no
// TCP port, no Docker) and returns a connected client plus a cleanup func.
func dial(t *testing.T, svc *supplier.Service) supplierv1.SupplierServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := NewGRPCServer(svc)
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
	client := dial(t, supplier.NewService(newFakeRepository()))

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
	client := dial(t, supplier.NewService(newFakeRepository()))

	_, err := client.CreateSupplier(context.Background(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestGetSupplier_NotFound(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()))

	_, err := client.GetSupplier(context.Background(), &supplierv1.GetSupplierRequest{Id: "missing"})

	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreateSupplier_MissingRequiredField_InvalidArgument(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()))
	write := validWrite()
	write.Name = ""

	_, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: write})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteSupplier_ThenGet_NotFound(t *testing.T) {
	client := dial(t, supplier.NewService(newFakeRepository()))
	created, err := client.CreateSupplier(adminCtx(), &supplierv1.CreateSupplierRequest{Supplier: validWrite()})
	require.NoError(t, err)

	_, err = client.DeleteSupplier(adminCtx(), &supplierv1.DeleteSupplierRequest{Id: created.GetSupplier().GetId()})
	require.NoError(t, err)

	_, err = client.GetSupplier(context.Background(), &supplierv1.GetSupplierRequest{Id: created.GetSupplier().GetId()})
	require.Equal(t, codes.NotFound, status.Code(err))
}
