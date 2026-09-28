// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-28
// Scope: New file. Pins the public REST URLs the gateway exposes for each
//   RPC (D-037) by reading the google.api.http options off the compiled
//   service descriptor.
// Author review: PENDING — <reviewer to complete>

package grpcapi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	supplierv1 "foc/supplier-service/internal/gen/supplier/v1"
)

// route is the verb, path and body/response mapping of one RPC's
// google.api.http rule.
type route struct {
	verb, path, body, responseBody string
}

func routeOf(t *testing.T, rule *annotations.HttpRule) route {
	t.Helper()
	r := route{body: rule.GetBody(), responseBody: rule.GetResponseBody()}
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		r.verb, r.path = "GET", p.Get
	case *annotations.HttpRule_Post:
		r.verb, r.path = "POST", p.Post
	case *annotations.HttpRule_Put:
		r.verb, r.path = "PUT", p.Put
	case *annotations.HttpRule_Delete:
		r.verb, r.path = "DELETE", p.Delete
	default:
		t.Fatalf("unexpected HTTP pattern %T", p)
	}
	return r
}

func TestPublicRoutes(t *testing.T) {
	want := map[string]route{
		"ListSuppliers":  {verb: "GET", path: "/api/suppliers", responseBody: "suppliers"},
		"GetSupplier":    {verb: "GET", path: "/api/suppliers/{id}", responseBody: "supplier"},
		"CreateSupplier": {verb: "POST", path: "/api/suppliers", body: "supplier", responseBody: "supplier"},
		"UpdateSupplier": {verb: "PUT", path: "/api/suppliers/{id}", body: "supplier", responseBody: "supplier"},
		"DeleteSupplier": {verb: "DELETE", path: "/api/suppliers/{id}"},
	}

	methods := supplierv1.File_foc_supplier_v1_supplier_proto.Services().ByName("SupplierService").Methods()
	require.Equal(t, len(want), methods.Len(), "a new RPC needs a route decision recorded before it is added here")

	for name, expected := range want {
		m := methods.ByName(protoreflect.Name(name))
		require.NotNil(t, m, name)
		rule, ok := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
		require.True(t, ok && rule != nil, "%s has no google.api.http rule", name)
		require.Equal(t, expected, routeOf(t, rule), name)
	}
}

// The frontend sends ?q=; grpc-gateway matches a query parameter to a field
// by proto name and then by JSON name.
func TestListSuppliers_SearchIsQueriedAsQ(t *testing.T) {
	f := (&supplierv1.ListSuppliersRequest{}).ProtoReflect().Descriptor().Fields()
	require.Equal(t, "q", string(f.ByName("search").JSONName()))
	require.Equal(t, "category", string(f.ByName("category").JSONName()))
}
