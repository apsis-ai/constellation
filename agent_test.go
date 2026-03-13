package mux

import (
	"os"
	"strings"
	"testing"
)

func TestResolveAgentBinary_Claude(t *testing.T) {
	// claude should resolve to claude on PATH (or error if not installed)
	path, err := resolveAgentBinary("claude")
	if err != nil {
		// Acceptable in CI where claude might not be installed
		if !strings.Contains(err.Error(), "not found on PATH") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestResolveAgentBinary_Nonexistent(t *testing.T) {
	// Override PATH to ensure the binary is not found
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent_dir_only")
	defer os.Setenv("PATH", origPath)

	_, err := resolveAgentBinary("claude")
	if err == nil {
		t.Fatal("expected error when binary not on PATH")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("expected 'not found on PATH' in error, got: %v", err)
	}
}

func TestBuildAttachmentPrompt_NoAttachments(t *testing.T) {
	result := buildAttachmentPrompt("hello", nil)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestBuildAttachmentPrompt_WithAttachments(t *testing.T) {
	atts := []AttachmentRef{
		{Path: "/tmp/file1.txt"},
		{Path: "/tmp/file2.png"},
	}
	result := buildAttachmentPrompt("test prompt", atts)
	if !strings.Contains(result, "/tmp/file1.txt") {
		t.Error("expected file1.txt in prompt")
	}
	if !strings.Contains(result, "/tmp/file2.png") {
		t.Error("expected file2.png in prompt")
	}
	if !strings.Contains(result, "test prompt") {
		t.Error("expected original prompt in result")
	}
}

func TestFallbackTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"one two three four five six seven", "one two three four five"},
		{"short", "short"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := fallbackTitle(tc.input)
			if result != tc.expected {
				t.Errorf("fallbackTitle(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestParseStatusMarker(t *testing.T) {
	tests := []struct {
		input          string
		expectCleaned  string
		expectStatus   string
	}{
		{"hello [STATUS: working] world", "hello  world", "working"},
		{"no marker here", "no marker here", ""},
		{"[STATUS: reading file]", "", "reading file"},
	}
	for _, tc := range tests {
		cleaned, status := parseStatusMarker(tc.input)
		if cleaned != tc.expectCleaned {
			t.Errorf("parseStatusMarker(%q) cleaned=%q, want %q", tc.input, cleaned, tc.expectCleaned)
		}
		if status != tc.expectStatus {
			t.Errorf("parseStatusMarker(%q) status=%q, want %q", tc.input, status, tc.expectStatus)
		}
	}
}
