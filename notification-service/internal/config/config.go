// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Reads notification-service's environment once (root AGENTS.md §4
//   rule 4).
// Author review: PENDING — <reviewer to complete>

// Package config reads notification-service's environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	defaultPort          = "8086"
	defaultConfirmWindow = 24 * time.Hour
)

// Config is everything notification-service reads from its environment.
type Config struct {
	Port        string
	DatabaseURL string
	RabbitMQURL string
	JWKSURL     string
	RedisURL    string
	// ConfirmWindow is C, the time a delivered errand waits before completing
	// by itself, which the delivered notification states (F5.2.3, D-141).
	ConfirmWindow time.Duration
}

// Load reads the environment. A missing required variable or an unusable
// window is an error.
func Load() (Config, error) {
	cfg := Config{
		Port:          os.Getenv("PORT"),
		DatabaseURL:   os.Getenv("NOTIFICATION_DB_URL"),
		RabbitMQURL:   os.Getenv("RABBITMQ_URL"),
		JWKSURL:       os.Getenv("JWKS_URL"),
		RedisURL:      os.Getenv("REDIS_URL"),
		ConfirmWindow: defaultConfirmWindow,
	}
	if cfg.Port == "" {
		cfg.Port = defaultPort
	}
	var missing []error
	for name, value := range map[string]string{
		"NOTIFICATION_DB_URL": cfg.DatabaseURL,
		"RABBITMQ_URL":        cfg.RabbitMQURL,
		"JWKS_URL":            cfg.JWKSURL,
		"REDIS_URL":           cfg.RedisURL,
	} {
		if value == "" {
			missing = append(missing, fmt.Errorf("%s is required", name))
		}
	}
	if err := errors.Join(missing...); err != nil {
		return Config{}, err
	}
	if raw := os.Getenv("ORDER_CONFIRMATION_WINDOW"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("ORDER_CONFIRMATION_WINDOW must be a positive duration, got %q", raw)
		}
		cfg.ConfirmWindow = d
	}
	return cfg, nil
}
