package config

import "os"

type Config struct {
	Port        string
	DatabaseURL string
	SeedCSVPath string

	// AI-generated (edited by James Teo).
	// OrderBaseURL is order-service, asked before every delete whether an
	// unfinished errand still references the supplier (FR F2.2.3).
	OrderBaseURL string
}

func Load() Config {
	return Config{
		Port:        getenv("PORT", "8082"),
		DatabaseURL: getenv("SUPPLIER_DB_URL", "postgres://postgres:postgres@localhost:5432/supplier?sslmode=disable"),
		SeedCSVPath: getenv("SEED_CSV_PATH", "../data/csv/supplier-seed-data.csv"),

		// AI-generated (edited by James Teo).
		OrderBaseURL: getenv("ORDER_BASE_URL", "http://localhost:8083"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
