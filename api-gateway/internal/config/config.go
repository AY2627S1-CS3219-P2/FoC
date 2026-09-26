// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Config plumbing for the gateway — reads env once, returns a struct.
//   Reworked for D-023 (RS256/JWKS replaces the symmetric secret) and D-024
//   (the gateway is not a Redis client, so RedisURL is gone).
//   2026-09-22: RefreshTokenTTL added — the gateway now owns the refresh
//   token's cookie and needs its Max-Age — and StaticDir, the built
//   frontend it serves so the browser is same-origin.
// Author review: Nigeltzy - Checked the output of this implementation code and the AI is confirmed to have generated a valid config.
// and filled in the config as per standard procedure.

// Package config reads the gateway's environment once at startup and returns a
// Config. Nothing else in the service reads os.Getenv.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Config is the gateway's runtime configuration, as returned by Load.
type Config struct {
	// Port is the port the gateway listens on (PORT).
	Port string

	// JWKSURL is user-service's JWKS endpoint, from which the gateway fetches the
	// RSA public keys it verifies access tokens with (JWKS_URL).
	JWKSURL string

	// RefreshTokenTTL is the Max-Age of the refresh-token cookie
	// (REFRESH_TOKEN_TTL). It must equal user-service's JWT_REFRESH_TOKEN_TTL:
	// shorter logs users out early, longer leaves a cookie the refresh call rejects.
	RefreshTokenTTL time.Duration

	// StaticDir is the directory of built frontend files served at "/" for any
	// path no other route matches (STATIC_DIR). Optional: empty serves no pages.
	StaticDir string

	// Downstream holds the base URL of each service the gateway forwards to.
	Downstream Downstream
}

// Downstream is the set of services the gateway forwards to, one base URL each,
// read from <SERVICE>_BASE_URL. Adding a service means a field here, its
// variable in Load, and a row in httpapi's serviceRoutes.
type Downstream struct {
	User     string
	Supplier string
	Order    string
	Credit   string
}

// Load reads the environment and returns an error naming every required
// variable that is unset or blank, or a REFRESH_TOKEN_TTL that is not a Go
// duration. There are no defaults; STATIC_DIR is the only optional variable.
func Load() (Config, error) {
	rawTTL := os.Getenv("REFRESH_TOKEN_TTL")
	ttl, err := time.ParseDuration(rawTTL)
	if rawTTL != "" && err != nil {
		return Config{}, fmt.Errorf("config: REFRESH_TOKEN_TTL %q is not a duration: %w", rawTTL, err)
	}

	cfg := Config{
		Port:            os.Getenv("PORT"),
		JWKSURL:         os.Getenv("JWKS_URL"),
		RefreshTokenTTL: ttl,
		StaticDir:       os.Getenv("STATIC_DIR"),
		Downstream: Downstream{
			User:     os.Getenv("USER_BASE_URL"),
			Supplier: os.Getenv("SUPPLIER_BASE_URL"),
			Order:    os.Getenv("ORDER_BASE_URL"),
			Credit:   os.Getenv("CREDIT_BASE_URL"),
		},
	}

	required := map[string]string{
		"PORT":              cfg.Port,
		"JWKS_URL":          cfg.JWKSURL,
		"USER_BASE_URL":     cfg.Downstream.User,
		"SUPPLIER_BASE_URL": cfg.Downstream.Supplier,
		"ORDER_BASE_URL":    cfg.Downstream.Order,
		"CREDIT_BASE_URL":   cfg.Downstream.Credit,
		"REFRESH_TOKEN_TTL": rawTTL,
	}

	var missing []string
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	// Sorted because Go randomises map iteration order: without this the
	// error text varies run to run, which makes it untestable.
	sort.Strings(missing)
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: required environment variables not set: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}
