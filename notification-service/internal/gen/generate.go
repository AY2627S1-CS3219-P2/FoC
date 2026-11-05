// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Package doc and the go:generate directive, as in order-service.
// Author review: PENDING — <reviewer to complete>

// Package gen holds the Go types and chi server interface generated from
// api/openapi.yaml. Never edit notification.gen.go by hand: change the spec and
// run `go generate ./internal/gen`.
package gen

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml ../../api/openapi.yaml
