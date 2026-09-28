// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. Transport-layer gRPC handlers, transcoding the REST
//   surface that was in internal/httpapi (removed this same session)
//   field-for-field and RPC-for-RPC — no new capability.
//   2026-09-28: RPCs return per-RPC response messages (D-040).
// Author review: PENDING — <reviewer to complete>

// Package grpcapi is the transport layer: it decodes requests, calls the
// service, and maps errors to status codes — no business rules (root
// AGENTS.md §6). Successor to internal/httpapi, removed 2026-09-22 when
// the service moved from REST to gRPC as its only public contract.
package grpcapi

import (
	"context"

	supplierv1 "foc/supplier-service/internal/gen/supplier/v1"
	"foc/supplier-service/internal/supplier"
)

// Server implements supplierv1.SupplierServiceServer.
type Server struct {
	supplierv1.UnimplementedSupplierServiceServer
	svc *supplier.Service
}

func NewServer(svc *supplier.Service) *Server {
	return &Server{svc: svc}
}

// ListSuppliers handles FR F2.1.1, F2.1.3.
func (s *Server) ListSuppliers(ctx context.Context, req *supplierv1.ListSuppliersRequest) (*supplierv1.ListSuppliersResponse, error) {
	suppliers, err := s.svc.List(ctx, supplier.ListFilter{
		Category: req.GetCategory(),
		Search:   req.GetSearch(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &supplierv1.ListSuppliersResponse{Suppliers: toProtoList(suppliers)}, nil
}

// GetSupplier handles FR F2.1.2.
func (s *Server) GetSupplier(ctx context.Context, req *supplierv1.GetSupplierRequest) (*supplierv1.GetSupplierResponse, error) {
	sup, err := s.svc.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return &supplierv1.GetSupplierResponse{Supplier: toProto(sup)}, nil
}

// CreateSupplier handles FR F2.2 (admin-only — enforced by the
// middleware.RequireAdmin interceptor, not here).
func (s *Server) CreateSupplier(ctx context.Context, req *supplierv1.CreateSupplierRequest) (*supplierv1.CreateSupplierResponse, error) {
	created, err := s.svc.Create(ctx, toDomain(req.GetSupplier()))
	if err != nil {
		return nil, toStatus(err)
	}
	return &supplierv1.CreateSupplierResponse{Supplier: toProto(created)}, nil
}

// UpdateSupplier handles FR F2.2 (admin-only).
func (s *Server) UpdateSupplier(ctx context.Context, req *supplierv1.UpdateSupplierRequest) (*supplierv1.UpdateSupplierResponse, error) {
	updated, err := s.svc.Update(ctx, req.GetId(), toDomain(req.GetSupplier()))
	if err != nil {
		return nil, toStatus(err)
	}
	return &supplierv1.UpdateSupplierResponse{Supplier: toProto(updated)}, nil
}

// DeleteSupplier handles FR F2.2.3 (admin-only). See the comment on
// supplier.Service.Delete for why this is a soft delete.
func (s *Server) DeleteSupplier(ctx context.Context, req *supplierv1.DeleteSupplierRequest) (*supplierv1.DeleteSupplierResponse, error) {
	if err := s.svc.Delete(ctx, req.GetId()); err != nil {
		return nil, toStatus(err)
	}
	return &supplierv1.DeleteSupplierResponse{}, nil
}
