// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated system endpoint handlers and their narrow dependencies.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"net/http"
)

// JWKSProvider returns the public JSON Web Key Set for the API gateway.
type JWKSProvider interface{ JWKS() ([]byte, error) }

// HealthCheck reports whether a required service dependency is healthy.
type HealthCheck func(context.Context) error

// SystemDependencies contains only the operations required by SystemHandler.
type SystemDependencies struct {
	JWKSProvider JWKSProvider
	HealthCheck  HealthCheck
}

// SystemHandler serves service-health and key-distribution endpoints.
type SystemHandler struct{ deps SystemDependencies }

// NewSystemHandler constructs a system handler with its required operations.
func NewSystemHandler(deps SystemDependencies) SystemHandler { return SystemHandler{deps: deps} }

// Health handles the service health endpoint.
func (h SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
	if h.deps.HealthCheck != nil && h.deps.HealthCheck(r.Context()) != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// JWKS handles public key-set distribution.
func (h SystemHandler) JWKS(w http.ResponseWriter, _ *http.Request) {
	if h.deps.JWKSProvider == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWKS unavailable"})
		return
	}
	payload, err := h.deps.JWKSProvider.JWKS()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "JWKS unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
