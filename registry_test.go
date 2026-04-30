package mux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRegistry_DefaultAgents(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	r := NewRegistry(m.providers)
	agents := r.ListAgents()
	if len(agents) == 0 {
		t.Fatal("expected at least one default agent")
	}

	found := false
	for _, a := range agents {
		if a.ID == "claude" {
			found = true
			if a.Name == "" {
				t.Error("expected non-empty name for claude")
			}
		}
	}
	if !found {
		t.Error("expected 'claude' in default agents")
	}
}

func TestRegistry_RegisterAgent(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	r := NewRegistry(m.providers)
	r.Register(AgentInfo{
		ID:        "custom-agent",
		Name:      "Custom Agent",
		Available: true,
	})

	agents := r.ListAgents()
	found := false
	for _, a := range agents {
		if a.ID == "custom-agent" {
			found = true
			if a.Name != "Custom Agent" {
				t.Errorf("expected name 'Custom Agent', got %q", a.Name)
			}
		}
	}
	if !found {
		t.Error("expected 'custom-agent' after Register")
	}
}

func TestRegistry_GetAgent(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	r := NewRegistry(m.providers)
	a, ok := r.GetAgent("claude")
	if !ok {
		t.Fatal("expected claude to be found")
	}
	if a.ID != "claude" {
		t.Errorf("expected ID 'claude', got %q", a.ID)
	}

	_, ok = r.GetAgent("nonexistent")
	if ok {
		t.Error("expected nonexistent agent to not be found")
	}
}

func TestRegistry_DefaultAgentList(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	r := NewRegistry(m.providers)
	agents := r.ListAgents()

	expectedIDs := map[string]bool{
		"claude":   false,
		"codex":    false,
		"opencode": false,
		"agent":    false,
	}
	for _, a := range agents {
		if _, want := expectedIDs[a.ID]; want {
			expectedIDs[a.ID] = true
		}
	}
	for id, found := range expectedIDs {
		if !found {
			t.Errorf("expected agent %q in default list", id)
		}
	}
}

func TestRegistry_ListAgentsIncludesDiscoveredProviderModels(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	binary := fakeModelBinary(t, "openai/gpt-5.3\ngithub-copilot/gpt-5.4\n")
	if err := m.providers.Register(CLIProviderConfig{
		ProviderID: "opencode",
		Name:       "OpenCode",
		Binary:     binary,
		ParserType: "opencode",
		ModelDiscovery: &ModelDiscoveryConfig{
			Command: []string{"models"},
			Format:  "lines",
		},
	}); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(m.providers)
	agents := r.ListAgents()

	opencode, ok := findAgent(agents, "opencode")
	if !ok {
		t.Fatal("expected opencode agent")
	}
	if !containsString(opencode.Models, "openai/gpt-5.3") {
		t.Fatalf("expected discovered OpenCode model in registry, got %#v", opencode.Models)
	}
	if !containsString(opencode.Models, "github-copilot/gpt-5.4") {
		t.Fatalf("expected discovered GitHub Copilot model in registry, got %#v", opencode.Models)
	}
}

func TestRegistry_GetAgentIncludesDiscoveredProviderModels(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	binary := fakeModelBinary(t, "openai/gpt-5.3\n")
	if err := m.providers.Register(CLIProviderConfig{
		ProviderID: "opencode",
		Name:       "OpenCode",
		Binary:     binary,
		ParserType: "opencode",
		ModelDiscovery: &ModelDiscoveryConfig{
			Command: []string{"models"},
			Format:  "lines",
		},
	}); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(m.providers)

	opencode, ok := r.GetAgent("opencode")
	if !ok {
		t.Fatal("expected opencode agent")
	}
	if !containsString(opencode.Models, "openai/gpt-5.3") {
		t.Fatalf("expected discovered model from GetAgent, got %#v", opencode.Models)
	}
}

func findAgent(agents []AgentInfo, id string) (AgentInfo, bool) {
	for _, agent := range agents {
		if agent.ID == id {
			return agent, true
		}
	}
	return AgentInfo{}, false
}

func fakeModelBinary(t *testing.T, output string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "models-cli")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"models\" ]; then\n" +
		"  cat <<'EOF'\n" +
		output +
		"EOF\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
