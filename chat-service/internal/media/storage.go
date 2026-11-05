package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

var (
	ErrFileNotFound = errors.New("media file not found")
	ErrInvalidMedia = errors.New("file is not a valid JPEG or PNG image")
)

type Storage struct {
	baseDir string
	mu      sync.RWMutex
}

func NewStorage(baseDir string) (*Storage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create media base directory: %w", err)
	}
	return &Storage{baseDir: baseDir}, nil
}

// Store writes the image to disk after verifying magic bytes. Returns detected MIME type.
func (s *Storage) Store(ctx context.Context, mediaID uuid.UUID, r io.Reader) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read first 512 bytes for MIME detection
	header := make([]byte, 512)
	n, err := io.ReadFull(r, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read header: %w", err)
	}
	header = header[:n]

	mimeType := http.DetectContentType(header)
	if mimeType != "image/jpeg" && mimeType != "image/png" {
		return "", ErrInvalidMedia
	}

	ext := ".jpg"
	if mimeType == "image/png" {
		ext = ".png"
	}

	filePath := filepath.Join(s.baseDir, mediaID.String()+ext)
	out, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	// Write header first
	if _, err := io.Copy(out, bytes.NewReader(header)); err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("write file header: %w", err)
	}

	// Write remainder
	if _, err := io.Copy(out, r); err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("write file body: %w", err)
	}

	return mimeType, nil
}

// Open retrieves the image file and its detected content type.
func (s *Storage) Open(ctx context.Context, mediaID uuid.UUID) (io.ReadCloser, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ext := range []string{".jpg", ".png"} {
		filePath := filepath.Join(s.baseDir, mediaID.String()+ext)
		f, err := os.Open(filePath)
		if err == nil {
			mimeType := "image/jpeg"
			if ext == ".png" {
				mimeType = "image/png"
			}
			return f, mimeType, nil
		}
	}
	return nil, "", ErrFileNotFound
}

// Delete removes the image file from disk.
func (s *Storage) Delete(ctx context.Context, mediaID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ext := range []string{".jpg", ".png"} {
		filePath := filepath.Join(s.baseDir, mediaID.String()+ext)
		_ = os.Remove(filePath)
	}
	return nil
}
