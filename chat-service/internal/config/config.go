package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port             string
	ChatDBURL        string
	ChatRedisURL     string
	AuthRedisURL     string
	JWKSURL          string
	RabbitMQURL      string
	MediaStoragePath string
	MigrationsDir    string
}

func Load() (*Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	chatDBURL := os.Getenv("CHAT_DB_URL")
	if chatDBURL == "" {
		return nil, fmt.Errorf("CHAT_DB_URL is required")
	}

	chatRedisURL := os.Getenv("CHAT_REDIS_URL")
	if chatRedisURL == "" {
		return nil, fmt.Errorf("CHAT_REDIS_URL is required")
	}

	authRedisURL := os.Getenv("AUTH_REDIS_URL")
	if authRedisURL == "" {
		authRedisURL = os.Getenv("REDIS_URL")
		if authRedisURL == "" {
			return nil, fmt.Errorf("AUTH_REDIS_URL is required")
		}
	}

	jwksURL := os.Getenv("JWKS_URL")
	if jwksURL == "" {
		return nil, fmt.Errorf("JWKS_URL is required")
	}

	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}

	mediaPath := os.Getenv("MEDIA_STORAGE_PATH")
	if mediaPath == "" {
		mediaPath = "/var/lib/foc/chat-media"
	}

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}

	return &Config{
		Port:             port,
		ChatDBURL:        chatDBURL,
		ChatRedisURL:     chatRedisURL,
		AuthRedisURL:     authRedisURL,
		JWKSURL:          jwksURL,
		RabbitMQURL:      rabbitmqURL,
		MediaStoragePath: mediaPath,
		MigrationsDir:    migrationsDir,
	}, nil
}
