// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-22
// Scope: New file. Domain<->proto mapping, transcoding the former
//   internal/httpapi/dto.go field-for-field.
// Author review: PENDING — <reviewer to complete>

package grpcapi

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	supplierv1 "foc/supplier-service/internal/gen/supplier/v1"
	"foc/supplier-service/internal/supplier"
)

// toDomain converts a SupplierWrite (the create/update request body) into
// a domain Supplier. Matches the former httpapi.supplierRequest.toDomain:
// proto3 has no field-presence for a plain bool, so unlike the REST DTO's
// *bool there is no "omitted means default true" case here — the caller
// must send is_available explicitly. This is a real, if minor, behavior
// difference from the REST API and is called out in the service's README.
func toDomain(w *supplierv1.SupplierWrite) *supplier.Supplier {
	return &supplier.Supplier{
		Name:                w.GetName(),
		Type:                w.GetType(),
		Building:            w.GetBuilding(),
		Floor:               w.GetFloor(),
		LocationDescription: w.GetLocationDescription(),
		Latitude:            w.GetLatitude(),
		Longitude:           w.GetLongitude(),
		OpeningTime:         w.GetOpeningTime(),
		ClosingTime:         w.GetClosingTime(),
		ImageURL:            w.GetImageUrl(),
		Description:         w.GetDescription(),
		IsAvailable:         w.GetIsAvailable(),
	}
}

// toProto converts a domain Supplier into the wire message. Matches the
// former httpapi.toResponse.
func toProto(s *supplier.Supplier) *supplierv1.Supplier {
	return &supplierv1.Supplier{
		Id:                  s.ID,
		Name:                s.Name,
		Type:                s.Type,
		Building:            s.Building,
		Floor:               s.Floor,
		LocationDescription: s.LocationDescription,
		Latitude:            s.Latitude,
		Longitude:           s.Longitude,
		OpeningTime:         s.OpeningTime,
		ClosingTime:         s.ClosingTime,
		ImageUrl:            s.ImageURL,
		Description:         s.Description,
		IsAvailable:         s.IsAvailable,
		CreatedAt:           timestamppb.New(s.CreatedAt),
		UpdatedAt:           timestamppb.New(s.UpdatedAt),
	}
}

// toProtoList converts a slice of domain Suppliers. Matches the former
// httpapi.toResponseList.
func toProtoList(in []*supplier.Supplier) []*supplierv1.Supplier {
	out := make([]*supplierv1.Supplier, 0, len(in))
	for _, s := range in {
		out = append(out, toProto(s))
	}
	return out
}
