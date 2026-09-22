// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Config plumbing for the gateway — reads env once, returns a struct.
//   Reworked for D-023 (RS256/JWKS replaces the symmetric secret) and D-024
//   (the gateway is not a Redis client, so RedisURL is gone).
//   2026-09-22: RefreshTokenTTL added — the gateway now owns the refresh
//   token's cookie and needs its Max-Age.
// Author review: PENDING — <reviewer to complete>

// Package config reads the gateway's environment once at startup and returns
// an immutable Config. Nothing else in the service reads os.Getenv, and there
// is no package-level state (root AGENTS.md §4.4, §5).
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Config is the gateway's complete runtime configuration. Load returns a
// ready-to-use value; there is no Init or Start to call afterwards
// (root AGENTS.md §5, temporal coupling).
type Config struct {
	// Port the gateway listens on. This is the ONLY publicly reachable port
	// in the system (D-010).
	Port string

	// JWKSURL is user-service's JSON Web Key Set endpoint, from which the
	// gateway fetches the RSA public keys it verifies access tokens with
	// (D-023).
	//
	// The gateway holds no private key and cannot mint a token, which is what
	// keeps user-service the sole issuer (D-012).
	JWKSURL string

	// RefreshTokenTTL is how long the refresh-token cookie lives, and must
	// match user-service's JWT_REFRESH_TOKEN_TTL. The gateway sets that
	// cookie, so it needs the lifetime; it does not issue the token itself
	// and cannot derive it from one, because the RT is opaque here.
	//
	// A mismatch is one-directional and not symmetric: too short logs the
	// user out early, too long leaves a cookie that fails at the exchange.
	RefreshTokenTTL time.Duration

	// Downstream holds one base URL per callee, per root AGENTS.md §3.
	Downstream Downstream
}

// Downstream is the set of services the gateway forwards to. One base URL per
// callee, named <SERVICE>_BASE_URL (root AGENTS.md §3).
//
// This is the gateway's extension point: a new service means a field here, an
// env var, and a prefix in the proxy's route table (D-027). Nothing else in
// the gateway needs to know it exists.
type Downstream struct {
	User     string
	Supplier string
	Order    string
	Credit   string
}

// Load reads the environment and validates that every required variable is
// present. It returns an error rather than falling back to a default: the
// gateway's port is provisional (D-018), and an invented default would
// silently manufacture a decision nobody made.
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
