// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Package doc and the go:generate directive that runs oapi-codegen
//   v2.8.0 on api/openapi.yaml (D-005, D-053).
// Author review: James Teo — reviewed and verified.

// Package gen holds the Go types and chi server interface generated from
// api/openapi.yaml (D-005, D-053). Never edit order.gen.go by hand: change the
// spec and run `go generate ./internal/gen`.
package gen

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml ../../api/openapi.yaml
