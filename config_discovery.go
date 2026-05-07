package mux

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DiscoveryCacheEntry struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Options   []ConfigOption `json:"options"`
}

type ConfigDiscoveryResolver struct {
	CacheDir string
	TTL      time.Duration
}

func DefaultDiscoveryCacheDir() string {
	if v := os.Getenv("CONSTELLATION_DISCOVERY_CACHE_DIR"); v != "" {
		return v
	}
	if c, err := os.UserCacheDir(); err == nil {
		return filepath.Join(c, "constellation", "discovery")
	}
	return filepath.Join(os.TempDir(), "constellation", "discovery")
}

func NewConfigDiscoveryResolver(cacheDir string, ttl time.Duration) ConfigDiscoveryResolver {
	if cacheDir == "" {
		cacheDir = DefaultDiscoveryCacheDir()
	}
	if ttl == 0 {
		ttl = 10 * time.Minute
	}
	return ConfigDiscoveryResolver{CacheDir: cacheDir, TTL: ttl}
}

func (r ConfigDiscoveryResolver) Resolve(ctx context.Context, file ProviderFileConfig, field ConfigField, force bool) ([]ConfigOption, string) {
	if field.OptionsSource == nil {
		return field.Options, ""
	}
	key := file.ID + "-" + field.Key + ".json"
	if !force {
		if entry, ok := r.readFresh(key); ok {
			return entry.Options, ""
		}
	}
	options, err := r.discover(ctx, file, field)
	if err == nil && len(options) > 0 {
		_ = r.write(key, DiscoveryCacheEntry{FetchedAt: time.Now().UTC(), Options: options})
		return options, ""
	}
	if entry, ok := r.readAny(key); ok {
		return entry.Options, "discovery failed, using stale cache from " + entry.FetchedAt.Format(time.RFC3339)
	}
	if len(field.FallbackOptions) > 0 {
		return field.FallbackOptions, "discovery failed, using fallback options"
	}
	return nil, "discovery failed and no fallback options are configured"
}

func (r ConfigDiscoveryResolver) discover(ctx context.Context, file ProviderFileConfig, field ConfigField) ([]ConfigOption, error) {
	source := field.OptionsSource
	switch source.Type {
	case OptionSourceModelDiscovery:
		cfg := CLIProviderConfig{ProviderID: file.ID, Name: file.Name, Binary: file.Binary, ParserType: file.ParserType, Models: file.Models, Efforts: file.Efforts, ModelDiscovery: file.Discovery}
		if len(source.Command) > 0 || source.Format != "" {
			cfg.ModelDiscovery = &ModelDiscoveryConfig{Command: source.Command, Format: source.Format}
		}
		models, err := NewCLIProvider(cfg, nil).ListModels(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]ConfigOption, 0, len(models))
		for _, m := range models {
			out = append(out, ConfigOption{Value: m.ID, Label: firstNonEmpty(m.Name, m.ID), Meta: map[string]any{"provider": strings.Split(m.ID, "/")[0], "efforts": m.Efforts}})
		}
		return out, nil
	case OptionSourceDerivedFromModels:
		sep := source.Separator
		if sep == "" {
			sep = "/"
		}
		seen := map[string]bool{}
		out := []ConfigOption{}
		for _, model := range file.Models {
			parts := strings.Split(model, sep)
			idx := source.Segment
			if idx >= 0 && idx < len(parts) && !seen[parts[idx]] {
				seen[parts[idx]] = true
				out = append(out, ConfigOption{Value: parts[idx], Label: parts[idx]})
			}
		}
		return out, nil
	default:
		return field.Options, nil
	}
}

func (r ConfigDiscoveryResolver) readFresh(key string) (DiscoveryCacheEntry, bool) {
	entry, ok := r.readAny(key)
	return entry, ok && time.Since(entry.FetchedAt) < r.TTL
}

func (r ConfigDiscoveryResolver) readAny(key string) (DiscoveryCacheEntry, bool) {
	data, err := os.ReadFile(filepath.Join(r.CacheDir, key))
	if err != nil {
		return DiscoveryCacheEntry{}, false
	}
	var entry DiscoveryCacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return DiscoveryCacheEntry{}, false
	}
	return entry, true
}

func (r ConfigDiscoveryResolver) write(key string, entry DiscoveryCacheEntry) error {
	if err := os.MkdirAll(r.CacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.CacheDir, key), append(data, '\n'), 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
