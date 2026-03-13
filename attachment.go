package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// UploadConfig holds configurable limits for attachment uploads.
type UploadConfig struct {
	MaxAttachments   int      // default 5
	MaxImageSize     int64    // default 5MB
	MaxDocSize       int64    // default 10MB
	MaxTotalSize     int64    // default 20MB
	AllowedImageExts []string // default: .jpg, .jpeg, .png, .gif, .webp
}

// DefaultUploadConfig returns the default upload configuration.
func DefaultUploadConfig() UploadConfig {
	return UploadConfig{
		MaxAttachments:   5,
		MaxImageSize:     5 << 20,  // 5MB
		MaxDocSize:       10 << 20, // 10MB
		MaxTotalSize:     20 << 20, // 20MB
		AllowedImageExts: []string{".jpg", ".jpeg", ".png", ".gif", ".webp"},
	}
}

// AttachmentUpload represents a file to be saved as an attachment.
// This is the library-friendly equivalent of multipart.FileHeader.
type AttachmentUpload struct {
	Filename string
	Data     []byte
	Size     int64
}

// Validate checks uploads against the configured limits.
func (c UploadConfig) Validate(uploads []AttachmentUpload) error {
	if len(uploads) > c.MaxAttachments {
		return fmt.Errorf("too many attachments: %d (max %d)", len(uploads), c.MaxAttachments)
	}
	var totalSize int64
	for _, u := range uploads {
		ext := strings.ToLower(filepath.Ext(u.Filename))
		isImage := false
		for _, ie := range c.AllowedImageExts {
			if ext == ie {
				isImage = true
				break
			}
		}
		if isImage && u.Size > c.MaxImageSize {
			return fmt.Errorf("image %s too large: %d bytes (max %d)", u.Filename, u.Size, c.MaxImageSize)
		}
		if !isImage && u.Size > c.MaxDocSize {
			return fmt.Errorf("document %s too large: %d bytes (max %d)", u.Filename, u.Size, c.MaxDocSize)
		}
		totalSize += u.Size
	}
	if totalSize > c.MaxTotalSize {
		return fmt.Errorf("total upload size too large: %d bytes (max %d)", totalSize, c.MaxTotalSize)
	}
	return nil
}

// attCounter tracks per-session monotonic attachment counters to avoid ID collisions.
var attCounter sync.Map // map[string]*atomic.Int64

// nextAttachmentID returns the next unique attachment ID for a session.
func nextAttachmentID(sessionID string) string {
	val, _ := attCounter.LoadOrStore(sessionID, &atomic.Int64{})
	counter := val.(*atomic.Int64)
	n := counter.Add(1)
	return fmt.Sprintf("att_%d", n)
}

// AttachmentDir returns the attachments subdirectory for a session, creating it if needed.
func (m *Manager) AttachmentDir(sessionID string) string {
	dir := filepath.Join(m.sessionDir(sessionID), "attachments")
	os.MkdirAll(dir, 0755)
	return dir
}

// SaveAttachmentBytes validates, saves uploaded files to the session attachment directory,
// and returns attachment references. This is the library-friendly API that accepts raw bytes.
func (m *Manager) SaveAttachmentBytes(sessionID string, uploads []AttachmentUpload) ([]AttachmentRef, error) {
	attDir := m.AttachmentDir(sessionID)
	var attachments []AttachmentRef

	for _, u := range uploads {
		id := nextAttachmentID(sessionID)
		// Sanitize filename: take base name, replace spaces
		safeName := strings.ReplaceAll(filepath.Base(u.Filename), " ", "_")
		diskName := fmt.Sprintf("%s_%s", id, safeName)
		destPath := filepath.Join(attDir, diskName)

		// Use O_CREATE|O_EXCL to prevent overwriting existing files
		dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to save %s: %w", u.Filename, err)
		}
		_, writeErr := dst.Write(u.Data)
		dst.Close()
		if writeErr != nil {
			return nil, fmt.Errorf("failed to write %s: %w", u.Filename, writeErr)
		}

		ext := strings.ToLower(filepath.Ext(u.Filename))
		attType := "document"
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp":
			attType = "image"
		}

		attachments = append(attachments, AttachmentRef{
			ID:   id,
			Name: u.Filename,
			Type: attType,
			Size: u.Size,
		})
	}
	return attachments, nil
}
