package mux

import (
	"sort"
	"testing"
)

func TestParserRegistry_GetParser(t *testing.T) {
	// Arrange
	reg := NewParserRegistry()

	// Act & Assert - known parsers
	for _, name := range []string{"claude", "codex", "opencode", "cursor", "other"} {
		factory, ok := reg.GetParser(name)
		if !ok {
			t.Errorf("expected parser %q to be registered", name)
			continue
		}
		if factory == nil {
			t.Errorf("expected non-nil factory for %q", name)
		}
	}

	// Act & Assert - unknown parser
	_, ok := reg.GetParser("unknown")
	if ok {
		t.Error("expected GetParser('unknown') to return false")
	}
}

func TestParserRegistry_FactoryCreatesParser(t *testing.T) {
	// Arrange
	reg := NewParserRegistry()
	cb, _ := testCallbacks()

	// Act
	factory, ok := reg.GetParser("claude")
	if !ok {
		t.Fatal("expected claude parser to be registered")
	}
	parser := factory(cb)

	// Assert
	if parser == nil {
		t.Fatal("expected non-nil parser")
	}
	if _, ok := parser.(*ClaudeParser); !ok {
		t.Errorf("expected *ClaudeParser, got %T", parser)
	}
}

func TestParserRegistry_OtherReturnsRawParser(t *testing.T) {
	// Arrange
	reg := NewParserRegistry()
	cb, _ := testCallbacks()

	// Act
	factory, ok := reg.GetParser("other")
	if !ok {
		t.Fatal("expected 'other' parser to be registered")
	}
	parser := factory(cb)

	// Assert
	if _, ok := parser.(*RawParser); !ok {
		t.Errorf("expected *RawParser, got %T", parser)
	}
}

func TestParserRegistry_List(t *testing.T) {
	// Arrange
	reg := NewParserRegistry()

	// Act
	names := reg.List()

	// Assert
	sort.Strings(names)
	expected := []string{"claude", "codex", "cursor", "opencode", "other"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d parsers, got %d: %v", len(expected), len(names), names)
	}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("expected name[%d]=%q, got %q", i, name, names[i])
		}
	}
}

func TestParserRegistry_Register(t *testing.T) {
	// Arrange
	reg := NewParserRegistry()

	// Act - register a custom parser
	reg.Register("custom", func(cb ParserCallbacks) OutputParser {
		return &RawParser{Callbacks: cb}
	})

	// Assert
	factory, ok := reg.GetParser("custom")
	if !ok {
		t.Fatal("expected 'custom' parser to be registered")
	}
	if factory == nil {
		t.Fatal("expected non-nil factory")
	}
}
