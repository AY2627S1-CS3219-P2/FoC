// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Reads order-service's environment once into a Config (root AGENTS.md
//   §4.4), with T, K and C defaulting to D-049's values, and the user-service
//   key and Redis URLs that per-service authentication needs (D-054, D-056).
// Author review: James Teo — reviewed and verified.

// Package config reads order-service's environment once, at startup.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config is everything order-service reads from its environment.
type Config struct {
	Port            string
	DatabaseURL     string
	SupplierBaseURL string
	CreditBaseURL   string
	RabbitMQURL     string
	// JWKSURL is where user-service publishes its signing keys (D-023, D-056).
	JWKSURL string
	// RedisURL is the shared Redis holding user-service's revocation keys (D-054).
	RedisURL string
	// ExpiryPeriod, MaxActive and ConfirmWindow are T, K and C (D-049).
	ExpiryPeriod  time.Duration
	MaxActive     int
	ConfirmWindow time.Duration
}

// Load reads the environment, applying defaults for local development. It
// fails only on a value that is set but malformed.
func Load() (Config, error) {
	cfg := Config{
		Port:            getenv("PORT", "8083"),
		DatabaseURL:     getenv("ORDER_DB_URL", "postgres://postgres:postgres@localhost:5432/orders?sslmode=disable"),
		SupplierBaseURL: getenv("SUPPLIER_BASE_URL", "http://localhost:8082"),
		CreditBaseURL:   getenv("CREDIT_BASE_URL", "http://localhost:8084"),
		RabbitMQURL:     getenv("RABBITMQ_URL", "amqp://foc:foc-dev-password@localhost:5672/foc"),
		JWKSURL:         getenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json"),
		RedisURL:        getenv("REDIS_URL", "redis://localhost:6379"),
	}
	// The broker URL carries its password, so this error is fixed text and
	// never wraps or quotes the value: net/url's error quotes the whole URL,
	// and even its inner error (invalid port ":<password>" after host) can
	// quote the password. Parsing with net/url rather than the AMQP client
	// keeps that dependency at the relay's edge (root AGENTS.md §5).
	if u, err := url.Parse(cfg.RabbitMQURL); err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") {
		return Config{}, errors.New("RABBITMQ_URL must be an amqp:// or amqps:// URL with reserved characters in the user and password percent-encoded")
	}
	// Fixed text for the same reason: a Redis URL can carry a password.
	if u, err := url.Parse(cfg.RedisURL); err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") {
		return Config{}, errors.New("REDIS_URL must be a redis:// or rediss:// URL with reserved characters in the user and password percent-encoded")
	}
	if u, err := url.Parse(cfg.JWKSURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, fmt.Errorf("JWKS_URL must be an http:// or https:// URL, got %q", cfg.JWKSURL)
	}
	var err error
	if cfg.ExpiryPeriod, err = durationEnv("ORDER_EXPIRY_PERIOD", 2*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.ConfirmWindow, err = durationEnv("ORDER_CONFIRMATION_WINDOW", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.MaxActive, err = positiveIntEnv("ORDER_MAX_ACTIVE_ASSIGNMENTS", 2); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 2h or 30s, got %q", key, v)
	}
	return d, nil
}

func positiveIntEnv(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a whole number of at least 1, got %q", key, v)
	}
	return n, nil
}
