// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented the isolated standard-library HTTP handler for JWKS distribution.
// Author review: ZI YANG - validated correctness

// Package jwkshttp serves the user service's HTTP-only key-discovery exception.
package jwkshttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/google/uuid"
)

// Path is the standard JSON Web Key Set discovery path.
const Path = "/.well-known/jwks.json"

// Provider returns the public JSON Web Key Set for the API gateway.
type Provider interface {
	JWKS() ([]byte, error)
}

type handler struct {
	provider Provider
	logger   *slog.Logger
}

// NewHandler constructs the isolated JWKS HTTP surface.
func NewHandler(provider Provider, logger *slog.Logger) (http.Handler, error) {
	if provider == nil {
		return nil, errors.New("JWKS provider is required")
	}
	if logger == nil {
		return nil, errors.New("JWKS logger is required")
	}

	h := handler{provider: provider, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc(Path, h.serve)
	return mux, nil
}

func (h handler) serve(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	payload, err := h.provider.JWKS()
	if err != nil {
		h.logger.Error("JWKS unavailable",
			"request_id", uuid.NewString(),
			"error", err.Error(),
			"stack_trace", string(debug.Stack()),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "JWKS unavailable"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
