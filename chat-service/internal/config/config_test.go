package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiredFields(t *testing.T) {
	os.Clearenv()

	_, err := Load()
	assert.Error(t, err)

	os.Setenv("CHAT_DB_URL", "postgres://chat:chat@localhost:5432/chat")
	os.Setenv("CHAT_REDIS_URL", "redis://localhost:6379")
	os.Setenv("AUTH_REDIS_URL", "redis://localhost:6380")
	os.Setenv("JWKS_URL", "http://localhost:8081/.well-known/jwks.json")
	os.Setenv("RABBITMQ_URL", "amqp://localhost:5672")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "8085", cfg.Port)
	assert.Equal(t, "postgres://chat:chat@localhost:5432/chat", cfg.ChatDBURL)
	assert.Equal(t, "/var/lib/foc/chat-media", cfg.MediaStoragePath)
}
