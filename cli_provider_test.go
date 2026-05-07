package mux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIProvider_BuildArgs_Claude(t *testing.T) {
	parsers := NewParserRegistry()
	cfg := BuiltinCLIConfigs()[0] // claude
	p := NewCLIProvider(cfg, parsers)

	runtimeConfig := ProviderRuntimeConfig{Sections: []ConfigSection{{Fields: []ConfigField{{Key: "model", Mapping: &ExecutionMapping{Kind: "arg", Name: "--model"}}}}, {Fields: []ConfigField{{Key: "effort", Mapping: &ExecutionMapping{Kind: "arg", Name: "--effort"}}}}}}
	req := ProviderRequest{
		SessionID:      "test-session",
		Prompt:         "hello world",
		ConversationID: "conv-123",
		RuntimeConfig:  &runtimeConfig,
		ConfigValues:   map[string]any{"model": "opus", "effort": "high"},
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

	runtimeConfig := ProviderRuntimeConfig{Sections: []ConfigSection{{Fields: []ConfigField{{Key: "model", Mapping: &ExecutionMapping{Kind: "arg", Name: "-m"}}}}}}
	req := ProviderRequest{
		Prompt:        "fix the bug",
		RuntimeConfig: &runtimeConfig,
		ConfigValues:  map[string]any{"model": "o4-mini"},
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

func TestCLIProvider_BuildArgs_Pi(t *testing.T) {
	parsers := NewParserRegistry()
	cfg := BuiltinCLIConfigs()[3] // pi
	p := NewCLIProvider(cfg, parsers)

	runtimeConfig := ProviderRuntimeConfig{Sections: []ConfigSection{{Fields: []ConfigField{{Key: "model", Mapping: &ExecutionMapping{Kind: "arg", Name: "--model"}}}}, {Fields: []ConfigField{{Key: "effort", Mapping: &ExecutionMapping{Kind: "arg", Name: "--thinking"}}}}}}
	req := ProviderRequest{
		Prompt:         "write docs",
		ConversationID: "pi-session-123",
		RuntimeConfig:  &runtimeConfig,
		ConfigValues:   map[string]any{"model": "openai/gpt-5.2", "effort": "high"},
	}

	args := p.BuildArgs(req)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--mode json") {
		t.Errorf("expected --mode json, got: %s", joined)
	}
	if !strings.Contains(joined, "--model openai/gpt-5.2") {
		t.Errorf("expected --model openai/gpt-5.2, got: %s", joined)
	}
	if !strings.Contains(joined, "--thinking high") {
		t.Errorf("expected --thinking high, got: %s", joined)
	}
	if !strings.Contains(joined, "--session pi-session-123") {
		t.Errorf("expected --session pi-session-123, got: %s", joined)
	}
}

func TestCLIProvider_BuildArgsIgnoresLegacyModelWithoutRuntimeConfig(t *testing.T) {
	parsers := NewParserRegistry()
	cfg := BuiltinCLIConfigs()[0]
	p := NewCLIProvider(cfg, parsers)

	args := p.BuildArgs(ProviderRequest{Prompt: "hello", Model: "opus", Effort: "high"})
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "--model opus") || strings.Contains(joined, "--effort high") {
		t.Fatalf("expected legacy fields to be ignored without runtime config, got: %s", joined)
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

func TestCLIProvider_OpenCodeDiscoveryUsesProviderQualifiedLabels(t *testing.T) {
	cfg := BuiltinCLIConfigs()[2] // opencode
	cfg.Binary = fakeModelBinary(t, "openai/gpt-5.3\ngithub-copilot/gpt-5.4\n")
	p := NewCLIProvider(cfg, NewParserRegistry())

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	model, ok := findModel(models, "openai/gpt-5.3")
	if !ok {
		t.Fatalf("expected openai/gpt-5.3 in discovered models, got %#v", models)
	}
	if model.Name != "openai/gpt-5.3" {
		t.Fatalf("expected provider-qualified label, got %q", model.Name)
	}
}

func TestCLIProvider_ListModelsDeduplicatesAliasesAndDiscovery(t *testing.T) {
	cfg := CLIProviderConfig{
		ProviderID: "dedupe",
		Name:       "Dedupe",
		Binary:     fakeModelBinary(t, "alias\ndiscovered\n"),
		ParserType: "other",
		Models:     []string{"fallback"},
		ModelDiscovery: &ModelDiscoveryConfig{
			Command: []string{"models"},
			Format:  "lines",
			Aliases: []ModelInfo{{ID: "alias", Name: "Alias Label"}},
		},
	}
	p := NewCLIProvider(cfg, NewParserRegistry())

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 unique models, got %#v", models)
	}
	if models[0].ID != "alias" || models[0].Name != "Alias Label" {
		t.Fatalf("expected alias entry to win duplicate, got %#v", models[0])
	}
	if models[1].ID != "discovered" {
		t.Fatalf("expected discovered model, got %#v", models[1])
	}
}

func TestCLIProvider_PiDiscoveryParsesTable(t *testing.T) {
	cfg := BuiltinCLIConfigs()[3] // pi
	cfg.Binary = fakeModelBinary(t, "Warning: No models match pattern \"openai-codex/gpt-5\"\nprovider      model                context  max-out  thinking  images\nopenai-codex  gpt-5.2              272K     128K     yes       yes\ngoogle         gemini-3-pro         1M       64K      no        yes\n")
	p := NewCLIProvider(cfg, NewParserRegistry())

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	model, ok := findModel(models, "openai-codex/gpt-5.2")
	if !ok {
		t.Fatalf("expected openai-codex/gpt-5.2 in discovered models, got %#v", models)
	}
	if model.Name != "openai-codex/gpt-5.2" {
		t.Fatalf("expected full provider-qualified label, got %q", model.Name)
	}
	if len(model.Efforts) == 0 {
		t.Fatalf("expected thinking-capable Pi model to expose efforts")
	}
}

func TestCLIProviderBuildCommandUsesWorkingDirectory(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-cli")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho ok\n"), 0755); err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	p := NewCLIProvider(CLIProviderConfig{Binary: binary, ParserType: "other"}, NewParserRegistry())

	cmd, err := p.BuildCommand(ProviderRequest{Prompt: "hi", WorkingDirectory: workingDirectory})
	if err != nil {
		t.Fatal(err)
	}

	if cmd.Dir != workingDirectory {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, workingDirectory)
	}
}

func TestCLIProvider_SupportsResume(t *testing.T) {
	parsers := NewParserRegistry()
	configs := BuiltinCLIConfigs()
	expected := map[string]bool{
		"claude":   true,
		"codex":    false,
		"opencode": true,
		"pi":       true,
		"agent":    true,
	}
	for _, cfg := range configs {
		p := NewCLIProvider(cfg, parsers)
		if p.SupportsResume() != expected[cfg.ProviderID] {
			t.Errorf("%s: expected SupportsResume=%v", cfg.ProviderID, expected[cfg.ProviderID])
		}
	}
}

func findModel(models []ModelInfo, id string) (ModelInfo, bool) {
	for _, model := range models {
		if model.ID == id {
			return model, true
		}
	}
	return ModelInfo{}, false
}
