package mux

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestConfigDiscoveryResolverKeepsProviderDiscoveryCachePath(t *testing.T) {
	cachePath := writeCodexModelCache(t, "gpt-5.5")
	resolver := NewConfigDiscoveryResolver(t.TempDir(), time.Minute)
	file := ProviderFileConfig{
		ID:         "codex",
		Binary:     "codex",
		ParserType: "codex",
		Discovery:  &ModelDiscoveryConfig{CachePath: cachePath, Format: "codex-cache"},
	}
	field := ConfigField{
		Key:             "model",
		OptionsSource:   &OptionsSource{Type: OptionSourceModelDiscovery, Format: "codex-cache"},
		FallbackOptions: []ConfigOption{{Value: "gpt-5.4", Label: "gpt-5.4"}},
	}

	options, warning := resolver.Resolve(context.Background(), file, field, true)

	if warning != "" {
		t.Fatalf("expected no warning, got %q", warning)
	}
	if len(options) != 1 || options[0].Value != "gpt-5.5" {
		t.Fatalf("expected codex cache model, got %#v", options)
	}
}

func TestConfigDiscoveryResolverIgnoresPreFixCacheKey(t *testing.T) {
	cacheDir := t.TempDir()
	oldCache := DiscoveryCacheEntry{FetchedAt: time.Now().UTC(), Options: []ConfigOption{{Value: "gpt-5.4", Label: "gpt-5.4"}}}
	data, err := json.Marshal(oldCache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "codex-model.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewConfigDiscoveryResolver(cacheDir, time.Minute)
	field := ConfigField{Key: "model", OptionsSource: &OptionsSource{Type: OptionSourceModelDiscovery, Format: "codex-cache"}}
	file := ProviderFileConfig{ID: "codex", Binary: "codex", ParserType: "codex", Discovery: &ModelDiscoveryConfig{CachePath: writeCodexModelCache(t, "gpt-5.5"), Format: "codex-cache"}}

	options, _ := resolver.Resolve(context.Background(), file, field, false)

	if len(options) != 1 || options[0].Value != "gpt-5.5" {
		t.Fatalf("expected old cache key ignored, got %#v", options)
	}
}

func writeCodexModelCache(t *testing.T, model string) string {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "models_cache.json")
	cache := `{"models":[{"slug":"` + model + `","display_name":"` + model + `","visibility":"list"}]}`
	if err := os.WriteFile(cachePath, []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	return cachePath
}
