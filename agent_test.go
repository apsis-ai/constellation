package mux

import (
	"os"
	"path/filepath"
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

func TestBuildAgentPrompt_WithRuntimeContext(t *testing.T) {
	// Arrange
	context := "Use Perigee UI blocks when helpful."
	userPrompt := "Ask me with choices"

	// Act
	result := buildAgentPrompt(userPrompt, context)

	// Assert
	if !strings.Contains(result, "Runtime context:") {
		t.Fatalf("expected runtime context header, got %q", result)
	}
	if !strings.Contains(result, context) {
		t.Fatalf("expected context body, got %q", result)
	}
	if !strings.Contains(result, "User request:\n"+userPrompt) {
		t.Fatalf("expected original prompt under user request, got %q", result)
	}
}

func TestBuildAgentPrompt_WithoutRuntimeContextLeavesPromptUnchanged(t *testing.T) {
	// Arrange
	userPrompt := "plain request"

	// Act
	result := buildAgentPrompt(userPrompt, "  ")

	// Assert
	if result != userPrompt {
		t.Fatalf("expected unchanged prompt, got %q", result)
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
		ProviderID: "unknown-agent",
		Prompt:     "hello",
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
		ProviderID: "claude",
		Prompt:     "hello",
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

	for _, id := range []string{"claude", "codex", "opencode", "pi", "agent"} {
		_, ok := m.providers.GetCLIProvider(id)
		if !ok {
			t.Errorf("expected builtin provider %q in registry", id)
		}
	}
}

type recordingRuntimeProvider struct {
	request AgentRuntimeRequest
}

func (p *recordingRuntimeProvider) ContextForAgent(sessionID, providerID string) string {
	return "legacy context should not be used when runtime provider exists"
}

func (p *recordingRuntimeProvider) PrepareAgentRuntime(req AgentRuntimeRequest) (*AgentRuntime, error) {
	p.request = req
	return &AgentRuntime{
		Context: "Injected runtime context from Perigee",
		Env: map[string]string{
			"PERIGEE_RUNTIME_DIR": "/tmp/perigee-runtime-test",
		},
	}, nil
}

func TestSend_MergesPerCallEnvIntoAgentProcess(t *testing.T) {
	// Arrange
	binDir := t.TempDir()
	envPath := filepath.Join(binDir, "env.txt")
	fakeBin := filepath.Join(binDir, "fake-agent")
	script := "#!/bin/sh\nprintf '%s\n' \"$PER_CALL_ENV\" > \"" + envPath + "\"\necho done\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	if err := m.providers.Register(CLIProviderConfig{ProviderID: "fake-env", Name: "Fake Env", Binary: fakeBin, ParserType: "other"}); err != nil {
		t.Fatalf("register fake provider: %v", err)
	}

	// Act
	result, err := m.Send(SendRequest{
		SessionID:  "env-session",
		ProviderID: "fake-env",
		Prompt:     "check env",
		Env:        map[string]string{"PER_CALL_ENV": "from-send-request"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	for range result.Events {
	}

	// Assert
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if strings.TrimSpace(string(envBytes)) != "from-send-request" {
		t.Fatalf("expected per-call env to reach process, got %q", string(envBytes))
	}
}

func TestSend_PreparesAgentRuntimeWithWorkingDirectory(t *testing.T) {
	// Arrange
	binDir := t.TempDir()
	argsPath := filepath.Join(binDir, "args.txt")
	envPath := filepath.Join(binDir, "env.txt")
	fakeBin := filepath.Join(binDir, "fake-agent")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsPath + "\"\nprintf '%s\\n' \"$PERIGEE_RUNTIME_DIR\" > \"" + envPath + "\"\necho done\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	projectDir := t.TempDir()
	runtime := &recordingRuntimeProvider{}
	cfg := tempConfig(t)
	cfg.AgentContext = runtime
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	if err := m.providers.Register(CLIProviderConfig{ProviderID: "fake", Name: "Fake", Binary: fakeBin, ParserType: "other"}); err != nil {
		t.Fatalf("register fake provider: %v", err)
	}

	// Act
	result, err := m.Send(SendRequest{SessionID: "runtime-session", ProviderID: "fake", Prompt: "Tell me what you see", WorkingDirectory: projectDir})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	for range result.Events {
	}

	// Assert
	if runtime.request.SessionID != "runtime-session" {
		t.Fatalf("expected runtime session id, got %q", runtime.request.SessionID)
	}
	if runtime.request.ProviderID != "fake" {
		t.Fatalf("expected runtime provider id, got %q", runtime.request.ProviderID)
	}
	if runtime.request.WorkingDirectory != projectDir {
		t.Fatalf("expected runtime working directory %q, got %q", projectDir, runtime.request.WorkingDirectory)
	}
	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := string(argsBytes)
	if !strings.Contains(args, "Runtime context:\nInjected runtime context from Perigee") {
		t.Fatalf("expected runtime context in prompt args, got %q", args)
	}
	if !strings.Contains(args, "User request:\nTell me what you see") {
		t.Fatalf("expected original prompt in prompt args, got %q", args)
	}
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if strings.TrimSpace(string(envBytes)) != "/tmp/perigee-runtime-test" {
		t.Fatalf("expected runtime env to reach process, got %q", string(envBytes))
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
