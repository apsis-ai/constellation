package mux

import (
	"context"
	"testing"
	"time"
)

func TestConfigDiscoveryResolverUsesFallbackWhenDiscoveryFails(t *testing.T) {
	resolver := NewConfigDiscoveryResolver(t.TempDir(), time.Minute)
	field := ConfigField{Key: "model", OptionsSource: &OptionsSource{Type: OptionSourceModelDiscovery, Command: []string{"--bad"}, Format: "lines"}, FallbackOptions: []ConfigOption{{Value: "openai/gpt-5", Label: "GPT-5"}}}

	options, warning := resolver.Resolve(context.Background(), ProviderFileConfig{ID: "pi", Binary: "missing-binary", ParserType: "pi"}, field, true)

	if len(options) != 1 || options[0].Value != "openai/gpt-5" {
		t.Fatalf("expected fallback option, got %#v", options)
	}
	if warning == "" {
		t.Fatal("expected fallback warning")
	}
}
