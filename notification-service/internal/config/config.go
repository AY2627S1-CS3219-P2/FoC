// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Reads notification-service's environment once (root AGENTS.md §4
//   rule 4).
// Author review: nigeltzy - Checked that the implementation is as expected and seems to be correct.

// Package config reads notification-service's environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
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

// Load reads the environment. A missing required variable, a broker URL that
// does not parse or an unusable window is an error.
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
	// AI-generated (edited by nigeltzy).
	// The broker URL carries its password, so this error is fixed text and never
	// wraps or quotes the value: net/url's error quotes the whole URL, and even
	// its inner error (invalid port ":<password>" after host) can quote the
	// password. Unchecked, the same text would be in the consumer's reconnect
	// warning every few seconds. As in order-service's config.
	if u, err := url.Parse(cfg.RabbitMQURL); err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") {
		return Config{}, errors.New("RABBITMQ_URL must be an amqp:// or amqps:// URL with reserved characters in the user and password percent-encoded")
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
