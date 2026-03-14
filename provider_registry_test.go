package mux

import (
	"testing"
)

func TestProviderRegistry_RegisterBuiltins(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	parsers := NewParserRegistry()
	reg, err := NewProviderRegistry(m.db, parsers)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterBuiltins(); err != nil {
		t.Fatal(err)
	}

	providers := reg.List()
	if len(providers) != 4 {
		t.Errorf("expected 4 providers, got %d", len(providers))
	}

	for _, id := range []string{"claude", "codex", "opencode", "agent"} {
		p, ok := reg.Get(id)
		if !ok {
			t.Errorf("expected provider %s to be registered", id)
			continue
		}
		if p.ID() != id {
			t.Errorf("expected ID %q, got %q", id, p.ID())
		}
	}
}

func TestProviderRegistry_SetEnabled(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	parsers := NewParserRegistry()
	reg, err := NewProviderRegistry(m.db, parsers)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterBuiltins(); err != nil {
		t.Fatal(err)
	}

	// Disable codex
	if err := reg.SetEnabled("codex", false); err != nil {
		t.Fatal(err)
	}

	// List should exclude codex
	providers := reg.List()
	for _, p := range providers {
		if p.ID() == "codex" {
			t.Error("codex should not be in List() after disabling")
		}
	}

	// ListAll should include codex
	all := reg.ListAll()
	found := false
	for _, info := range all {
		if info.ID == "codex" {
			found = true
			if info.Enabled {
				t.Error("codex should be disabled in ListAll()")
			}
		}
	}
	if !found {
		t.Error("codex should be in ListAll() even when disabled")
	}

	// Re-enable
	if err := reg.SetEnabled("codex", true); err != nil {
		t.Fatal(err)
	}
	_, ok := reg.Get("codex")
	if !ok {
		t.Error("codex should be gettable after re-enabling")
	}
}

func TestProviderRegistry_CustomProvider(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	parsers := NewParserRegistry()
	reg, err := NewProviderRegistry(m.db, parsers)
	if err != nil {
		t.Fatal(err)
	}

	custom := CLIProviderConfig{
		ProviderID: "my-agent",
		Name:       "My Custom Agent",
		Binary:     "my-agent-bin",
		BaseArgs:   []string{"run"},
		ParserType: "generic",
	}

	// Register generic parser for the test
	parsers.Register("generic", func(cb ParserCallbacks) OutputParser {
		return &CodexParser{Callbacks: cb} // reuse codex parser as generic
	})

	if err := reg.Register(custom); err != nil {
		t.Fatal(err)
	}

	p, ok := reg.Get("my-agent")
	if !ok {
		t.Fatal("expected custom provider to be registered")
	}
	if p.ID() != "my-agent" {
		t.Errorf("expected ID 'my-agent', got %q", p.ID())
	}
}

func TestProviderRegistry_Persistence(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	parsers := NewParserRegistry()
	reg, err := NewProviderRegistry(m.db, parsers)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterBuiltins(); err != nil {
		t.Fatal(err)
	}

	// Create a new registry from the same DB — should load persisted providers
	reg2, err := NewProviderRegistry(m.db, parsers)
	if err != nil {
		t.Fatal(err)
	}

	providers := reg2.List()
	if len(providers) != 4 {
		t.Errorf("expected 4 providers after reload, got %d", len(providers))
	}
}
