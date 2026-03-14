package mux

import (
	"strings"
	"testing"
)

func TestCLIProvider_BuildArgs_Claude(t *testing.T) {
	parsers := NewParserRegistry()
	cfg := BuiltinCLIConfigs()[0] // claude
	p := NewCLIProvider(cfg, parsers)

	req := ProviderRequest{
		SessionID:      "test-session",
		Prompt:         "hello world",
		Model:          "opus",
		Effort:         "high",
		ConversationID: "conv-123",
	}

	args := p.BuildArgs(req)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--model opus") {
		t.Errorf("expected --model opus, got: %s", joined)
	}
	if !strings.Contains(joined, "--effort high") {
		t.Errorf("expected --effort high, got: %s", joined)
	}
	if !strings.Contains(joined, "--resume conv-123") {
		t.Errorf("expected --resume conv-123, got: %s", joined)
	}
	if !strings.Contains(joined, "-- hello world") {
		t.Errorf("expected -- hello world, got: %s", joined)
	}
	if !strings.Contains(joined, "--output-format stream-json") {
		t.Errorf("expected --output-format stream-json, got: %s", joined)
	}
}

func TestCLIProvider_BuildArgs_Codex(t *testing.T) {
	parsers := NewParserRegistry()
	cfg := BuiltinCLIConfigs()[1] // codex
	p := NewCLIProvider(cfg, parsers)

	req := ProviderRequest{
		Prompt: "fix the bug",
		Model:  "o4-mini",
	}

	args := p.BuildArgs(req)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "exec --json") {
		t.Errorf("expected exec --json, got: %s", joined)
	}
	if !strings.Contains(joined, "-m o4-mini") {
		t.Errorf("expected -m o4-mini, got: %s", joined)
	}
	// Codex should NOT have --resume since it doesn't support it
	if strings.Contains(joined, "--resume") {
		t.Errorf("codex should not have --resume, got: %s", joined)
	}
}

func TestCLIProvider_BuildArgs_OpenCode(t *testing.T) {
	parsers := NewParserRegistry()
	cfg := BuiltinCLIConfigs()[2] // opencode
	p := NewCLIProvider(cfg, parsers)

	req := ProviderRequest{
		Prompt:         "review code",
		ConversationID: "sess-456",
		Attachments:    []AttachmentRef{{Path: "/tmp/test.txt"}},
	}

	args := p.BuildArgs(req)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-s sess-456") {
		t.Errorf("expected -s sess-456, got: %s", joined)
	}
	if !strings.Contains(joined, "--file /tmp/test.txt") {
		t.Errorf("expected --file /tmp/test.txt, got: %s", joined)
	}
}

func TestCLIProvider_Validate(t *testing.T) {
	parsers := NewParserRegistry()
	// Test with a binary that doesn't exist
	cfg := CLIProviderConfig{
		ProviderID: "nonexistent",
		Binary:     "this-binary-does-not-exist-xyz",
	}
	p := NewCLIProvider(cfg, parsers)
	err := p.Validate()
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("expected 'not found on PATH' error, got: %v", err)
	}
}

func TestCLIProvider_ID(t *testing.T) {
	parsers := NewParserRegistry()
	for _, cfg := range BuiltinCLIConfigs() {
		p := NewCLIProvider(cfg, parsers)
		if p.ID() != cfg.ProviderID {
			t.Errorf("expected ID %q, got %q", cfg.ProviderID, p.ID())
		}
	}
}

func TestCLIProvider_SupportsResume(t *testing.T) {
	parsers := NewParserRegistry()
	configs := BuiltinCLIConfigs()
	expected := map[string]bool{
		"claude":   true,
		"codex":    false,
		"opencode": true,
		"agent":    true,
	}
	for _, cfg := range configs {
		p := NewCLIProvider(cfg, parsers)
		if p.SupportsResume() != expected[cfg.ProviderID] {
			t.Errorf("%s: expected SupportsResume=%v", cfg.ProviderID, expected[cfg.ProviderID])
		}
	}
}
