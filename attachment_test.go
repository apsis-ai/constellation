package mux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAttachmentDir_CreatesDirectory(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("att-dir-test")
	dir := m.AttachmentDir("att-dir-test")

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("expected attachment dir to be created: %s", dir)
	}
	if filepath.Base(dir) != "attachments" {
		t.Errorf("expected dir to end with 'attachments', got %q", filepath.Base(dir))
	}
}

func TestNextAttachmentID_Monotonic(t *testing.T) {
	id1 := nextAttachmentID("mono-test")
	id2 := nextAttachmentID("mono-test")
	if id1 == id2 {
		t.Errorf("expected unique IDs, got %q twice", id1)
	}
	if id1 != "att_1" {
		t.Errorf("expected first ID 'att_1', got %q", id1)
	}
	if id2 != "att_2" {
		t.Errorf("expected second ID 'att_2', got %q", id2)
	}
}

func TestNextAttachmentID_PerSession(t *testing.T) {
	idA := nextAttachmentID("session-a-unique")
	idB := nextAttachmentID("session-b-unique")
	// Both should start at att_1 for their respective sessions
	if idA != "att_1" {
		t.Errorf("expected 'att_1' for session-a, got %q", idA)
	}
	if idB != "att_1" {
		t.Errorf("expected 'att_1' for session-b, got %q", idB)
	}
}

func TestSaveAttachments_SavesFiles(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("save-att-test")

	// Create test files using SaveAttachmentBytes
	refs, err := m.SaveAttachmentBytes("save-att-test", []AttachmentUpload{
		{Filename: "test.png", Data: []byte("fake png data"), Size: 14},
		{Filename: "doc.pdf", Data: []byte("fake pdf data"), Size: 13},
	})
	if err != nil {
		t.Fatalf("SaveAttachmentBytes: %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	// Check first attachment
	if refs[0].Name != "test.png" {
		t.Errorf("expected name 'test.png', got %q", refs[0].Name)
	}
	if refs[0].Type != "image" {
		t.Errorf("expected type 'image', got %q", refs[0].Type)
	}
	if refs[0].Size != 14 {
		t.Errorf("expected size 14, got %d", refs[0].Size)
	}

	// Check second attachment
	if refs[1].Name != "doc.pdf" {
		t.Errorf("expected name 'doc.pdf', got %q", refs[1].Name)
	}
	if refs[1].Type != "document" {
		t.Errorf("expected type 'document', got %q", refs[1].Type)
	}

	// Verify files exist on disk
	attDir := m.AttachmentDir("save-att-test")
	entries, err := os.ReadDir(attDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files in attachment dir, got %d", len(entries))
	}
}

func TestResolveAttachments_ResolvesIDs(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("resolve-att-test")

	// Save attachments first
	refs, err := m.SaveAttachmentBytes("resolve-att-test", []AttachmentUpload{
		{Filename: "image.jpg", Data: []byte("jpg data"), Size: 8},
	})
	if err != nil {
		t.Fatalf("SaveAttachmentBytes: %v", err)
	}

	// Resolve them
	resolved, err := m.ResolveAttachments("resolve-att-test", []string{refs[0].ID})
	if err != nil {
		t.Fatalf("ResolveAttachments: %v", err)
	}

	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(resolved))
	}
	if resolved[0].Path == "" {
		t.Error("expected non-empty Path")
	}
	if resolved[0].Type != "image" {
		t.Errorf("expected type 'image', got %q", resolved[0].Type)
	}
	if resolved[0].Name != "image.jpg" {
		t.Errorf("expected name 'image.jpg', got %q", resolved[0].Name)
	}

	// Verify the file at Path is readable
	if _, err := os.Stat(resolved[0].Path); os.IsNotExist(err) {
		t.Errorf("resolved path does not exist: %s", resolved[0].Path)
	}
}

func TestResolveAttachments_NotFound(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("resolve-notfound")
	// Create the attachments dir so we get "not found" instead of "no dir"
	m.AttachmentDir("resolve-notfound")

	_, err = m.ResolveAttachments("resolve-notfound", []string{"att_999"})
	if err == nil {
		t.Fatal("expected error when resolving nonexistent attachment")
	}
}

func TestUploadConfig_Defaults(t *testing.T) {
	cfg := DefaultUploadConfig()
	if cfg.MaxAttachments != 5 {
		t.Errorf("expected MaxAttachments 5, got %d", cfg.MaxAttachments)
	}
	if cfg.MaxImageSize != 5<<20 {
		t.Errorf("expected MaxImageSize 5MB, got %d", cfg.MaxImageSize)
	}
	if cfg.MaxDocSize != 10<<20 {
		t.Errorf("expected MaxDocSize 10MB, got %d", cfg.MaxDocSize)
	}
	if cfg.MaxTotalSize != 20<<20 {
		t.Errorf("expected MaxTotalSize 20MB, got %d", cfg.MaxTotalSize)
	}
	if len(cfg.AllowedImageExts) == 0 {
		t.Error("expected non-empty AllowedImageExts")
	}
}

func TestValidateUpload_TooManyAttachments(t *testing.T) {
	cfg := DefaultUploadConfig()
	cfg.MaxAttachments = 1

	uploads := []AttachmentUpload{
		{Filename: "a.png", Data: []byte("x"), Size: 1},
		{Filename: "b.png", Data: []byte("y"), Size: 1},
	}
	err := cfg.Validate(uploads)
	if err == nil {
		t.Fatal("expected error for too many attachments")
	}
}

func TestValidateUpload_FileTooLarge(t *testing.T) {
	cfg := DefaultUploadConfig()
	cfg.MaxImageSize = 10 // 10 bytes

	uploads := []AttachmentUpload{
		{Filename: "big.png", Data: make([]byte, 20), Size: 20},
	}
	err := cfg.Validate(uploads)
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}
