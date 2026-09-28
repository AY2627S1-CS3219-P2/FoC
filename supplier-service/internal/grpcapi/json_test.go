// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-28
// Scope: New file. Pins the browser-facing JSON shape recorded in D-036:
//   snake_case field names, in both directions.
// Author review: PENDING — <reviewer to complete>

package grpcapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	supplierv1 "foc/supplier-service/internal/gen/supplier/v1"
)

// The gateway renders these messages with protojson. This fixes the names it
// will emit and accept, independent of any marshaler option on the gateway.
func TestSupplierJSON_UsesSnakeCaseNames(t *testing.T) {
	msg := &supplierv1.Supplier{
		Id: "abc", Name: "Cool Spot", Type: "Food", Building: "Com2", Floor: "1",
		LocationDescription: "Opp LT16", Latitude: 1.29, Longitude: 103.77,
		OpeningTime: "09:00", ClosingTime: "21:30", ImageUrl: "http://x/y.png",
		Description: "d", IsAvailable: true,
		CreatedAt: timestamppb.New(time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)),
		UpdatedAt: timestamppb.New(time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)),
	}

	raw, err := protojson.Marshal(msg)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	for _, key := range []string{
		"id", "name", "type", "building", "floor", "location_description",
		"latitude", "longitude", "opening_time", "closing_time", "image_url",
		"description", "is_available", "created_at", "updated_at",
	} {
		require.Contains(t, got, key)
	}
	for _, camel := range []string{"locationDescription", "openingTime", "closingTime", "imageUrl", "isAvailable", "createdAt", "updatedAt"} {
		require.NotContains(t, got, camel)
	}
	require.Equal(t, "2026-09-28T01:02:03Z", got["created_at"])
}

func TestSupplierWriteJSON_AcceptsSnakeCaseBody(t *testing.T) {
	body := []byte(`{"name":"X","type":"Food","building":"B","location_description":"L",` +
		`"latitude":1.3,"longitude":103.8,"opening_time":"09:00","closing_time":"18:00",` +
		`"image_url":"u","description":"d","is_available":true}`)

	var w supplierv1.SupplierWrite
	require.NoError(t, protojson.Unmarshal(body, &w))

	require.Equal(t, "L", w.GetLocationDescription())
	require.Equal(t, "09:00", w.GetOpeningTime())
	require.Equal(t, "u", w.GetImageUrl())
	require.True(t, w.GetIsAvailable())
}
