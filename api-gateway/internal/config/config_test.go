// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Unit tests for Load — the error path first, per root AGENTS.md §7.
// Author review: Nigeltzy - This file was generated alongside the config package, and I checked the output of the generated code and it seems to be a typical implementation of unit tests for the config package. The numbers and ports are chosen by the team and / or used as default values. For example, the
//   TTL is a value that the team decided upon (and is typically the norm).

package config_test

import (
	"strings"
	"testing"
	"time"

	"foc/api-gateway/internal/config"
)

// all returns a value for every variable Load requires. Tests blank out the
// ones they want missing.
func all() map[string]string {
	return map[string]string{
		"PORT":              "8080",
		"JWKS_URL":          "http://user-service:8081/.well-known/jwks.json",
		"USER_BASE_URL":     "http://user-service:8081",
		"SUPPLIER_BASE_URL": "http://supplier-service:8082",
		"ORDER_BASE_URL":    "http://order-service:8083",
		"CREDIT_BASE_URL":   "http://credit-service:8084",
		"REFRESH_TOKEN_TTL": "168h",
	}
}

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	// t.Setenv restores each variable when the test ends and panics under
	// t.Parallel, so no test leaks config into another.
	for _, name := range []string{
		"PORT", "JWKS_URL", "USER_BASE_URL",
		"SUPPLIER_BASE_URL", "ORDER_BASE_URL", "CREDIT_BASE_URL",
		"REFRESH_TOKEN_TTL",
	} {
		t.Setenv(name, vars[name])
	}
}

func TestLoadSucceedsWithEverythingSet(t *testing.T) {
	setEnv(t, all())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.Downstream.Supplier != "http://supplier-service:8082" {
		t.Errorf("Downstream.Supplier = %q", cfg.Downstream.Supplier)
	}
}

func TestLoadReportsMissingVariables(t *testing.T) {
	tests := []struct {
		name        string
		unset       []string
		wantInError []string
	}{
		{
			name:        "one missing",
			unset:       []string{"JWKS_URL"},
			wantInError: []string{"JWKS_URL"},
		},
		{
			name:        "several missing are all reported, not just the first",
			unset:       []string{"PORT", "ORDER_BASE_URL", "CREDIT_BASE_URL"},
			wantInError: []string{"CREDIT_BASE_URL", "ORDER_BASE_URL", "PORT"},
		},
		{
			name:        "whitespace counts as missing",
			unset:       []string{},
			wantInError: []string{"USER_BASE_URL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := all()
			for _, name := range tt.unset {
				vars[name] = ""
			}
			if tt.name == "whitespace counts as missing" {
				vars["USER_BASE_URL"] = "   "
			}
			setEnv(t, vars)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load: expected an error, got nil")
			}
			for _, want := range tt.wantInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestMissingVariablesAreListedInSortedOrder(t *testing.T) {
	vars := all()
	vars["PORT"] = ""
	vars["CREDIT_BASE_URL"] = ""
	vars["JWKS_URL"] = ""
	setEnv(t, vars)

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load: expected an error, got nil")
	}
	want := "CREDIT_BASE_URL, JWKS_URL, PORT"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

// AI-generated (edited by nigeltzy).
func TestRefreshTokenTTLIsParsed(t *testing.T) {
	setEnv(t, all())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := 168 * time.Hour; cfg.RefreshTokenTTL != want {
		t.Errorf("RefreshTokenTTL = %v, want %v", cfg.RefreshTokenTTL, want)
	}
}

func TestRefreshTokenTTLRejectsNonDurations(t *testing.T) {
	vars := all()
	// A bare number is the likely mistake: time.ParseDuration needs a unit, so
	// "604800" must be rejected, not read as seconds.
	vars["REFRESH_TOKEN_TTL"] = "604800"
	setEnv(t, vars)

	if _, err := config.Load(); err == nil {
		t.Fatal("Load: expected an error for a unitless duration, got nil")
	}
}
