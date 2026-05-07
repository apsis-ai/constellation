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

func TestRuntimeConfigServiceDerivesCodexEffortsFromDiscoveredModels(t *testing.T) {
	// Arrange
	store := NewProviderFileStore(t.TempDir())
	codex := ProviderFileConfigFromCLI(BuiltinCLIConfigs()[1])
	codex.Discovery = &ModelDiscoveryConfig{CachePath: writeCodexModelCacheWithEfforts(t, "gpt-5.4", []string{"low", "medium", "high", "xhigh"}), Format: "codex-cache"}
	data, err := json.Marshal(codex)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "codex.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewRuntimeConfigService(store)
	service.Resolver = NewConfigDiscoveryResolver(t.TempDir(), time.Minute)

	// Act
	runtime, errs, err := service.List(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errs)
	}
	effortField := runtimeField(runtime, "codex", "effort")
	if effortField == nil {
		t.Fatal("expected effort field")
	}
	if !hasOption(effortField.Options, "xhigh") {
		t.Fatalf("expected xhigh effort from codex model cache, got %#v", effortField.Options)
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

	return writeCodexModelCacheWithEfforts(t, model, nil)
}

func writeCodexModelCacheWithEfforts(t *testing.T, model string, efforts []string) string {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "models_cache.json")
	reasoningLevels := make([]map[string]string, 0, len(efforts))
	for _, effort := range efforts {
		reasoningLevels = append(reasoningLevels, map[string]string{"effort": effort})
	}
	cacheData := map[string]any{"models": []map[string]any{{"slug": model, "display_name": model, "visibility": "list", "supported_reasoning_levels": reasoningLevels}}}
	data, err := json.Marshal(cacheData)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return cachePath
}

func runtimeField(providers []ProviderRuntimeConfig, providerID string, fieldKey string) *ConfigField {
	for pi := range providers {
		if providers[pi].ID != providerID {
			continue
		}
		for si := range providers[pi].Sections {
			for fi := range providers[pi].Sections[si].Fields {
				field := &providers[pi].Sections[si].Fields[fi]
				if field.Key == fieldKey {
					return field
				}
			}
		}
	}
	return nil
}

func hasOption(options []ConfigOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}
