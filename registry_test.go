package mux

import (
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
