// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Config plumbing for the gateway scaffold — reads env once, returns a
//   struct. No auth, routing or Redis logic.
// Author review: Edited by nigeltzy

// Package config reads the gateway's environment once at startup and returns
// an immutable Config. Nothing else in the service reads os.Getenv, and there
// is no package-level state (root AGENTS.md §4.4, §5).
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Config is the gateway's complete runtime configuration. Load returns a
// ready-to-use value; there is no Init or Start to call afterwards
// (root AGENTS.md §5, temporal coupling).
type Config struct {
	// Port the gateway listens on. This is the ONLY publicly reachable port
	// in the system (D-010).
	Port string

	// RedisURL addresses the revocation store the gateway checks on every
	// request (D-013).
	//
	// AI-generated (edited by PENDING) — rewritten for D-020, AFTER the
	// header above was signed off. Re-read this block before relying on that
	// signature.
	//
	// SETTLED: D-020 supersedes D-017. user-service is the sole writer and
	// this gateway only reads, so both processes hold this same URL.
	//
	// STILL OWED: that makes two services share one connection string, which
	// root AGENTS.md §4.1 forbids. D-020 is recorded as needing a written
	// carve-out for it, and nobody has written one. The field is here so
	// wiring compiles; nothing connects yet.
	RedisURL string

	// JWTSecret verifies access-token signatures (D-011, D-013).
	JWTSecret string

	// Downstream holds one base URL per callee, per root AGENTS.md §3.
	Downstream Downstream
}

// Downstream is the set of services the gateway forwards to. One base URL per
// callee, named <SERVICE>_BASE_URL (root AGENTS.md §3).
type Downstream struct {
	User     string
	Supplier string
	Order    string
	Credit   string
}

// Load reads the environment and validates that every required variable is
// present. It returns an error rather than falling back to a default: the
// gateway's port is not yet allocated in root AGENTS.md §3 or D-008, so an
// invented default would silently manufacture a decision nobody made.
func Load() (Config, error) {
	cfg := Config{
		Port:      os.Getenv("PORT"),
		RedisURL:  os.Getenv("REDIS_URL"),
		JWTSecret: os.Getenv("JWT_SECRET"),
		Downstream: Downstream{
			User:     os.Getenv("USER_BASE_URL"),
			Supplier: os.Getenv("SUPPLIER_BASE_URL"),
			Order:    os.Getenv("ORDER_BASE_URL"),
			Credit:   os.Getenv("CREDIT_BASE_URL"),
		},
	}

	required := map[string]string{
		"PORT":              cfg.Port,
		"REDIS_URL":         cfg.RedisURL,
		"JWT_SECRET":        cfg.JWTSecret,
		"USER_BASE_URL":     cfg.Downstream.User,
		"SUPPLIER_BASE_URL": cfg.Downstream.Supplier,
		"ORDER_BASE_URL":    cfg.Downstream.Order,
		"CREDIT_BASE_URL":   cfg.Downstream.Credit,
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
