// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-30
// Scope: Added EnableReflection, defaulting to on. Responds to a Copilot
//   review finding on PR #8 that gRPC reflection was registered
//   unconditionally.
// Author review: PENDING — <reviewer to complete>

package config

import "os"

type Config struct {
	Port        string
	DatabaseURL string
	SeedCSVPath string
	// EnableReflection controls whether grpc/reflection is registered.
	// Defaults to true: nothing in this repo is deployed anywhere but a
	// developer's machine yet (D-010's zones are enforced by compose.yaml
	// having no `ports:` key for this service, not by disabling
	// reflection), and it is what makes `grpcurl -plaintext` usable without
	// a copy of the .proto. Set SUPPLIER_DISABLE_REFLECTION=1 to turn it
	// off once there is an environment where that matters.
	EnableReflection bool
}

func Load() Config {
	return Config{
		Port:             getenv("PORT", "8082"),
		DatabaseURL:      getenv("SUPPLIER_DB_URL", "postgres://postgres:postgres@localhost:5432/supplier?sslmode=disable"),
		SeedCSVPath:      getenv("SEED_CSV_PATH", "../data/csv/supplier-seed-data.csv"),
		EnableReflection: getenv("SUPPLIER_DISABLE_REFLECTION", "") == "",
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
