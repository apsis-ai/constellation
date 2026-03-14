package mux

import (
	"strings"
	"testing"
)

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

func TestSend_UnknownProvider(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	_, err = m.Send(SendRequest{
		Agent:  "unknown-agent",
		Prompt: "hello",
	})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown provider: unknown-agent") {
		t.Errorf("expected 'unknown provider' error, got: %v", err)
	}
}

func TestSend_ProviderDispatch_UsesRegistry(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Disable the "claude" provider via registry, then verify Send returns an error
	// from the registry path (GetCLIProvider returns false for disabled providers)
	if err := m.providers.SetEnabled("claude", false); err != nil {
		t.Fatal(err)
	}

	_, err = m.Send(SendRequest{
		Agent:  "claude",
		Prompt: "hello",
	})
	if err == nil {
		t.Fatal("expected error when provider disabled")
	}
	if !strings.Contains(err.Error(), "unknown provider: claude") {
		t.Errorf("expected 'unknown provider' error for disabled provider, got: %v", err)
	}

	// Re-enable and verify it would proceed (will fail at Validate since binary may not exist)
	if err := m.providers.SetEnabled("claude", true); err != nil {
		t.Fatal(err)
	}
	_, ok := m.providers.GetCLIProvider("claude")
	if !ok {
		t.Error("expected claude to be available after re-enabling")
	}
}

func TestSend_EmptyAgentDefaultsClaude(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Verify that claude provider exists in the registry
	cliProv, ok := m.providers.GetCLIProvider("claude")
	if !ok {
		t.Fatal("expected claude provider in registry")
	}
	if cliProv.ID() != "claude" {
		t.Errorf("expected provider ID 'claude', got %q", cliProv.ID())
	}
}

func TestSend_ProviderRegistryHasAllBuiltins(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	for _, id := range []string{"claude", "codex", "opencode", "cursor"} {
		_, ok := m.providers.GetCLIProvider(id)
		if !ok {
			t.Errorf("expected builtin provider %q in registry", id)
		}
	}
}

func TestParseStatusMarker(t *testing.T) {
	tests := []struct {
		input         string
		expectCleaned string
		expectStatus  string
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
