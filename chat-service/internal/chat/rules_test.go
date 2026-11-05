package chat_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/chat-service/internal/chat"
)

func TestValidateTextMessage(t *testing.T) {
	t.Run("empty or whitespace", func(t *testing.T) {
		assert.Equal(t, chat.ErrEmptyMessage, chat.ValidateTextMessage(""))
		assert.Equal(t, chat.ErrEmptyMessage, chat.ValidateTextMessage("   \t\n "))
	})

	t.Run("valid standard ascii", func(t *testing.T) {
		assert.NoError(t, chat.ValidateTextMessage("Hello, on my way to deliver!"))
	})

	t.Run("250 unicode runes passes", func(t *testing.T) {
		// 250 multi-byte emojis
		msg := strings.Repeat("🍕", 250)
		assert.NoError(t, chat.ValidateTextMessage(msg))
	})

	t.Run("251 unicode runes rejected", func(t *testing.T) {
		msg := strings.Repeat("🍕", 251)
		assert.Equal(t, chat.ErrTextTooLong, chat.ValidateTextMessage(msg))
	})
}

func TestValidateImageMetadata(t *testing.T) {
	t.Run("valid jpeg and png under 10MB", func(t *testing.T) {
		assert.NoError(t, chat.ValidateImageMetadata("image/jpeg", 5*1024*1024))
		assert.NoError(t, chat.ValidateImageMetadata("image/png", 10*1024*1024))
	})

	t.Run("exceeds 10MB rejected", func(t *testing.T) {
		assert.Equal(t, chat.ErrImageTooLarge, chat.ValidateImageMetadata("image/jpeg", 10*1024*1024+1))
	})

	t.Run("unsupported mime rejected", func(t *testing.T) {
		assert.Equal(t, chat.ErrUnsupportedImageType, chat.ValidateImageMetadata("image/gif", 1024))
		assert.Equal(t, chat.ErrUnsupportedImageType, chat.ValidateImageMetadata("application/pdf", 1024))
	})
}

func TestConversationIsReadOnly(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	t.Run("active conversation is not read only", func(t *testing.T) {
		conv := chat.Conversation{
			Status: chat.StatusActive,
		}
		assert.False(t, conv.IsReadOnly(now))
	})

	t.Run("frozen conversation is always read only", func(t *testing.T) {
		conv := chat.Conversation{
			Status: chat.StatusFrozen,
		}
		assert.True(t, conv.IsReadOnly(now))
	})

	t.Run("completed 10 hours ago is still active", func(t *testing.T) {
		completedAt := now.Add(-10 * time.Hour)
		conv := chat.Conversation{
			Status:            chat.StatusActive,
			ErrandCompletedAt: &completedAt,
		}
		assert.False(t, conv.IsReadOnly(now))
	})

	t.Run("completed exactly 24 hours ago is read only", func(t *testing.T) {
		completedAt := now.Add(-24 * time.Hour)
		conv := chat.Conversation{
			Status:            chat.StatusActive,
			ErrandCompletedAt: &completedAt,
		}
		assert.True(t, conv.IsReadOnly(now))
	})

	t.Run("completed 25 hours ago is read only", func(t *testing.T) {
		completedAt := now.Add(-25 * time.Hour)
		conv := chat.Conversation{
			Status:            chat.StatusActive,
			ErrandCompletedAt: &completedAt,
		}
		assert.True(t, conv.IsReadOnly(now))
	})
}

func TestMessageCursor(t *testing.T) {
	now := time.Now().UTC()
	id := uuid.New()
	cursor := chat.MessageCursor{
		CreatedAt: now,
		ID:        id,
	}
	s := cursor.String()

	parsed, err := chat.ParseCursor(s)
	require.NoError(t, err)
	assert.Equal(t, id, parsed.ID)
	assert.True(t, now.Equal(parsed.CreatedAt))
}
