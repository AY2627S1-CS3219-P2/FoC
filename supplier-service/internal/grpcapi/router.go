// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. grpc.Server wiring: admin-role interceptor, the
//   standard health service, and reflection.
//   2026-09-30: reflection made conditional on enableReflection, responding
//   to a Copilot review finding on PR #8.
// Author review: PENDING — <reviewer to complete>

package grpcapi

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	supplierv1 "foc/supplier-service/internal/gen/supplier/v1"
	appmw "foc/supplier-service/internal/middleware"
	"foc/supplier-service/internal/supplier"
)

// adminMethods are the RPCs that require the ADMIN role (FR F2.2) — the
// gRPC equivalent of the REST router's admin-only route.Group.
var adminMethods = map[string]bool{
	"/foc.supplier.v1.SupplierService/CreateSupplier": true,
	"/foc.supplier.v1.SupplierService/UpdateSupplier": true,
	"/foc.supplier.v1.SupplierService/DeleteSupplier": true,
}

// NewGRPCServer wires the SupplierService implementation, the admin-role
// interceptor, the standard grpc.health.v1.Health service (for container
// readiness — the gRPC equivalent of the former GET /health), and,
// when enableReflection is true, reflection (so grpcurl and similar tools
// can call it without a copy of the .proto) into a *grpc.Server ready to
// Serve.
func NewGRPCServer(svc *supplier.Service, enableReflection bool) *grpc.Server {
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(appmw.RequireAdmin(appmw.MetadataRoleExtractor, adminMethods)),
	)

	supplierv1.RegisterSupplierServiceServer(srv, NewServer(svc))

	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	healthv1.RegisterHealthServer(srv, healthSrv)

	if enableReflection {
		reflection.Register(srv)
	}

	return srv
}
