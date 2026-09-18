// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the user-service command entry point placeholder.
// Author review: COMPLETED BY ZI YANG

package main

import (
	"context"
	"fmt"
	"log"

	"foc/user-service/internal/config"
	"foc/user-service/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	if err := run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cfg config.Config) error {
	if err := repository.Apply(cfg.UserDBURL, "file://migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.UserDBURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
