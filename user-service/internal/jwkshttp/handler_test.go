// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added focused tests for the isolated JWKS HTTP handler.
// Author review: ZI YANG - validate correctness

package jwkshttp_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/jwkshttp"
)

type providerStub struct {
	calls   int
	payload []byte
	err     error
}

func (s *providerStub) JWKS() ([]byte, error) {
	s.calls++
	return s.payload, s.err
}

func TestHandlerServesJWKSWithoutChi(t *testing.T) {
	provider := &providerStub{payload: []byte(`{"keys":[{"kid":"active"}]}`)}
	handler, err := jwkshttp.NewHandler(provider, slog.New(slog.NewJSONHandler(new(bytes.Buffer), nil)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, jwkshttp.Path, nil))

	if response.Code != http.StatusOK || response.Body.String() != string(provider.payload) {
		t.Fatalf("response = (%d, %q)", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}

func TestHandlerRejectsOtherMethodsAndPaths(t *testing.T) {
	provider := &providerStub{payload: []byte(`{"keys":[]}`)}
	handler, err := jwkshttp.NewHandler(provider, slog.New(slog.NewJSONHandler(new(bytes.Buffer), nil)))
	if err != nil {
		t.Fatal(err)
	}

	methodResponse := httptest.NewRecorder()
	handler.ServeHTTP(methodResponse, httptest.NewRequest(http.MethodPost, jwkshttp.Path, nil))
	if methodResponse.Code != http.StatusMethodNotAllowed || methodResponse.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method response = (%d, Allow %q)", methodResponse.Code, methodResponse.Header().Get("Allow"))
	}

	pathResponse := httptest.NewRecorder()
	handler.ServeHTTP(pathResponse, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if pathResponse.Code != http.StatusNotFound {
		t.Fatalf("path status = %d, want %d", pathResponse.Code, http.StatusNotFound)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestHandlerSanitizesAndLogsProviderFailure(t *testing.T) {
	provider := &providerStub{err: errors.New("private key store unavailable")}
	logs := new(bytes.Buffer)
	handler, err := jwkshttp.NewHandler(provider, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, jwkshttp.Path, nil))

	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"error\":\"JWKS unavailable\"}\n" {
		t.Fatalf("response = (%d, %q)", response.Code, response.Body.String())
	}
	if !strings.Contains(logs.String(), "private key store unavailable") {
		t.Fatalf("logs = %q, want provider error", logs.String())
	}
}

func TestNewHandlerRequiresDependencies(t *testing.T) {
	provider := &providerStub{}
	logger := slog.New(slog.NewJSONHandler(new(bytes.Buffer), nil))
	if _, err := jwkshttp.NewHandler(nil, logger); err == nil {
		t.Fatal("missing provider was accepted")
	}
	if _, err := jwkshttp.NewHandler(provider, nil); err == nil {
		t.Fatal("missing logger was accepted")
	}
}
