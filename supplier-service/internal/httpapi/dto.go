// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: A missing is_available in a create or update body is now false, not true.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"time"

	"foc/supplier-service/internal/supplier"
)

// supplierRequest is the JSON body accepted by create/update.
type supplierRequest struct {
	Name                string  `json:"name"`
	Type                string  `json:"type"`
	Building            string  `json:"building"`
	Floor               string  `json:"floor"`
	LocationDescription string  `json:"location_description"`
	Latitude            float64 `json:"latitude"`
	Longitude           float64 `json:"longitude"`
	OpeningTime         string  `json:"opening_time"`
	ClosingTime         string  `json:"closing_time"`
	ImageURL            string  `json:"image_url"`
	Description         string  `json:"description"`
	IsAvailable         *bool   `json:"is_available"`
}

// toDomain converts the body. A missing is_available is false: a supplier is
// available only when the body says so, and an admin can add one that starts
// unavailable.
func (req supplierRequest) toDomain() *supplier.Supplier {
	available := false
	if req.IsAvailable != nil {
		available = *req.IsAvailable
	}
	return &supplier.Supplier{
		Name:                req.Name,
		Type:                req.Type,
		Building:            req.Building,
		Floor:               req.Floor,
		LocationDescription: req.LocationDescription,
		Latitude:            req.Latitude,
		Longitude:           req.Longitude,
		OpeningTime:         req.OpeningTime,
		ClosingTime:         req.ClosingTime,
		ImageURL:            req.ImageURL,
		Description:         req.Description,
		IsAvailable:         available,
	}
}

// supplierResponse is the JSON representation returned to clients.
type supplierResponse struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	Building            string    `json:"building"`
	Floor               string    `json:"floor"`
	LocationDescription string    `json:"location_description"`
	Latitude            float64   `json:"latitude"`
	Longitude           float64   `json:"longitude"`
	OpeningTime         string    `json:"opening_time"`
	ClosingTime         string    `json:"closing_time"`
	ImageURL            string    `json:"image_url"`
	Description         string    `json:"description"`
	IsAvailable         bool      `json:"is_available"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func toResponse(s *supplier.Supplier) supplierResponse {
	return supplierResponse{
		ID:                  s.ID,
		Name:                s.Name,
		Type:                s.Type,
		Building:            s.Building,
		Floor:               s.Floor,
		LocationDescription: s.LocationDescription,
		Latitude:            s.Latitude,
		Longitude:           s.Longitude,
		OpeningTime:         s.OpeningTime,
		ClosingTime:         s.ClosingTime,
		ImageURL:            s.ImageURL,
		Description:         s.Description,
		IsAvailable:         s.IsAvailable,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	}
}

func toResponseList(in []*supplier.Supplier) []supplierResponse {
	out := make([]supplierResponse, 0, len(in))
	for _, s := range in {
		out = append(out, toResponse(s))
	}
	return out
}
