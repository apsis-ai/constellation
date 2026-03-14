package mux

// ParserFactory creates an OutputParser with the given callbacks.
type ParserFactory func(cb ParserCallbacks) OutputParser

// ParserRegistry holds registered parsers by name.
type ParserRegistry struct {
	parsers map[string]ParserFactory
}

// NewParserRegistry creates a registry with built-in parsers pre-registered.
func NewParserRegistry() *ParserRegistry {
	r := &ParserRegistry{
		parsers: make(map[string]ParserFactory),
	}
	r.Register("claude", func(cb ParserCallbacks) OutputParser { return &ClaudeParser{Callbacks: cb} })
	r.Register("codex", func(cb ParserCallbacks) OutputParser { return &CodexParser{Callbacks: cb} })
	r.Register("opencode", func(cb ParserCallbacks) OutputParser { return &OpenCodeParser{Callbacks: cb} })
	r.Register("cursor", func(cb ParserCallbacks) OutputParser { return &CursorParser{Callbacks: cb} })
	r.Register("other", func(cb ParserCallbacks) OutputParser { return &RawParser{Callbacks: cb} })
	return r
}

// Register adds a parser factory.
func (r *ParserRegistry) Register(name string, factory ParserFactory) {
	r.parsers[name] = factory
}

// GetParser returns a parser factory by name.
func (r *ParserRegistry) GetParser(name string) (ParserFactory, bool) {
	f, ok := r.parsers[name]
	return f, ok
}

// List returns all registered parser names.
func (r *ParserRegistry) List() []string {
	names := make([]string, 0, len(r.parsers))
	for name := range r.parsers {
		names = append(names, name)
	}
	return names
}
