package media_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/chat-service/internal/media"
)

// Minimal 1x1 PNG
var validPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// Minimal JPEG
var validJPEG = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01,
	0x01, 0x01, 0x00, 0x48, 0x00, 0x48, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43,
	0x00, 0xff, 0xd9,
}

func TestStorage_StoreOpenDelete(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "chat_media_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	s, err := media.NewStorage(tempDir)
	require.NoError(t, err)

	ctx := context.Background()
	pngID := uuid.New()

	t.Run("store valid png", func(t *testing.T) {
		mime, err := s.Store(ctx, pngID, bytes.NewReader(validPNG))
		require.NoError(t, err)
		assert.Equal(t, "image/png", mime)
	})

	t.Run("open stored png", func(t *testing.T) {
		rc, mime, err := s.Open(ctx, pngID)
		require.NoError(t, err)
		defer rc.Close()
		assert.Equal(t, "image/png", mime)

		content, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, validPNG, content)
	})

	t.Run("store valid jpeg", func(t *testing.T) {
		jpegID := uuid.New()
		mime, err := s.Store(ctx, jpegID, bytes.NewReader(validJPEG))
		require.NoError(t, err)
		assert.Equal(t, "image/jpeg", mime)
	})

	t.Run("reject non-image payload", func(t *testing.T) {
		invalidID := uuid.New()
		_, err := s.Store(ctx, invalidID, bytes.NewReader([]byte("plain text content here")))
		assert.Equal(t, media.ErrInvalidMedia, err)
	})

	t.Run("delete file", func(t *testing.T) {
		err := s.Delete(ctx, pngID)
		require.NoError(t, err)

		_, _, err = s.Open(ctx, pngID)
		assert.Equal(t, media.ErrFileNotFound, err)
	})
}
