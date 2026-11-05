package chat

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MaxTextMessageLength = 250
	MaxImageSizeBytes    = 10 * 1024 * 1024 // 10MB
)

var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrConversationReadOnly = errors.New("conversation is read-only")
	ErrUnauthorized         = errors.New("unauthorized to access conversation")
	ErrForbidden            = errors.New("forbidden")
	ErrTextTooLong          = errors.New("message exceeds 250 characters")
	ErrEmptyMessage         = errors.New("message cannot be empty")
	ErrImageTooLarge        = errors.New("image exceeds 10MB size limit")
	ErrUnsupportedImageType = errors.New("unsupported image format; only JPEG and PNG are allowed")
	ErrInvalidClientMessage = errors.New("invalid client message id")
)

// ValidateTextMessage validates non-empty and rune count <= 250.
func ValidateTextMessage(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ErrEmptyMessage
	}
	if utf8.RuneCountInString(text) > MaxTextMessageLength {
		return ErrTextTooLong
	}
	return nil
}

// ValidateImageMetadata validates file size and mime type.
func ValidateImageMetadata(mimeType string, sizeBytes int64) error {
	if sizeBytes <= 0 {
		return errors.New("image file cannot be empty")
	}
	if sizeBytes > MaxImageSizeBytes {
		return ErrImageTooLarge
	}
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg", "image/png":
		return nil
	default:
		return ErrUnsupportedImageType
	}
}
