package config

import "os"

type Config struct {
	Port        string
	DatabaseURL string
	SeedCSVPath string
}

func Load() Config {
	return Config{
		Port:        getenv("PORT", "8082"),
		DatabaseURL: getenv("SUPPLIER_DB_URL", "postgres://postgres:postgres@localhost:5432/supplier?sslmode=disable"),
		SeedCSVPath: getenv("SEED_CSV_PATH", "../data/csv/supplier-seed-data.csv"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
