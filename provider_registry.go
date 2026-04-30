package mux

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// ProviderRegistry manages providers with DB persistence and in-memory cache.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	configs   map[string]CLIProviderConfig
	enabled   map[string]bool
	parsers   *ParserRegistry
	db        *sql.DB
}

// NewProviderRegistry creates a registry backed by the given database.
func NewProviderRegistry(db *sql.DB, parsers *ParserRegistry) (*ProviderRegistry, error) {
	r := &ProviderRegistry{
		providers: make(map[string]Provider),
		configs:   make(map[string]CLIProviderConfig),
		enabled:   make(map[string]bool),
		parsers:   parsers,
		db:        db,
	}
	if err := r.initTable(); err != nil {
		return nil, fmt.Errorf("init providers table: %w", err)
	}
	if err := r.loadFromDB(); err != nil {
		return nil, fmt.Errorf("load providers: %w", err)
	}
	return r, nil
}

func (r *ProviderRegistry) initTable() error {
	_, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS providers (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		type        TEXT NOT NULL,
		parser_type TEXT,
		enabled     INTEGER DEFAULT 1,
		priority    INTEGER DEFAULT 0,
		config      TEXT NOT NULL,
		created_at  INTEGER,
		updated_at  INTEGER
	)`)
	return err
}

func (r *ProviderRegistry) loadFromDB() error {
	rows, err := r.db.Query(`SELECT id, name, type, parser_type, enabled, config FROM providers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, name, provType, parserType, configJSON string
		var enabled int
		if err := rows.Scan(&id, &name, &provType, &parserType, &enabled, &configJSON); err != nil {
			return err
		}
		if provType == "cli" {
			var cfg CLIProviderConfig
			if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
				continue
			}
			cfg.ProviderID = id
			cfg.Name = name
			cfg.ParserType = parserType
			r.configs[id] = cfg
			r.enabled[id] = enabled == 1
			if enabled == 1 {
				r.providers[id] = NewCLIProvider(cfg, r.parsers)
			}
		}
	}
	return rows.Err()
}

// Get returns a provider by ID (enabled only).
func (r *ProviderRegistry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// List returns all enabled providers.
func (r *ProviderRegistry) List() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}

// ProviderInfo is the external representation of a provider config.
type ProviderInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ParserType string `json:"parser_type"`
	Enabled    bool   `json:"enabled"`
}

// ListAll returns info for all providers including disabled.
func (r *ProviderRegistry) ListAll() []ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderInfo, 0, len(r.configs))
	for id, cfg := range r.configs {
		out = append(out, ProviderInfo{
			ID:         id,
			Name:       cfg.Name,
			Type:       "cli",
			ParserType: cfg.ParserType,
			Enabled:    r.enabled[id],
		})
	}
	return out
}

// Register adds or updates a provider config in DB and cache.
func (r *ProviderRegistry) Register(cfg CLIProviderConfig) error {
	return r.upsertConfig(cfg, true)
}

func (r *ProviderRegistry) upsertConfig(cfg CLIProviderConfig, enabled bool) error {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	now := nowUnix()
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err = r.db.Exec(`INSERT INTO providers (id, name, type, parser_type, enabled, config, created_at, updated_at)
		VALUES (?, ?, 'cli', ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=?, parser_type=?, enabled=?, config=?, updated_at=?`,
		cfg.ProviderID, cfg.Name, cfg.ParserType, enabledInt, string(configJSON), now, now,
		cfg.Name, cfg.ParserType, enabledInt, string(configJSON), now)
	if err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[cfg.ProviderID] = cfg
	r.enabled[cfg.ProviderID] = enabled
	if enabled {
		r.providers[cfg.ProviderID] = NewCLIProvider(cfg, r.parsers)
	} else {
		delete(r.providers, cfg.ProviderID)
	}
	return nil
}

// Unregister removes a provider from DB and cache.
func (r *ProviderRegistry) Unregister(id string) error {
	_, err := r.db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, id)
	delete(r.configs, id)
	delete(r.enabled, id)
	return nil
}

// SetEnabled enables or disables a provider.
func (r *ProviderRegistry) SetEnabled(id string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := r.db.Exec(`UPDATE providers SET enabled = ?, updated_at = ? WHERE id = ?`, enabledInt, nowUnix(), id)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled[id] = enabled
	if enabled {
		if cfg, ok := r.configs[id]; ok {
			r.providers[id] = NewCLIProvider(cfg, r.parsers)
		}
	} else {
		delete(r.providers, id)
	}
	return nil
}

// RegisterBuiltins seeds the DB with default providers if not already present.
func (r *ProviderRegistry) RegisterBuiltins() error {
	for _, cfg := range BuiltinCLIConfigs() {
		// Only insert if not already present (don't overwrite user customizations)
		var count int
		err := r.db.QueryRow(`SELECT COUNT(*) FROM providers WHERE id = ?`, cfg.ProviderID).Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			if err := r.Register(cfg); err != nil {
				return fmt.Errorf("register builtin %s: %w", cfg.ProviderID, err)
			}
		} else {
			r.mu.RLock()
			existingCfg, cfgLoaded := r.configs[cfg.ProviderID]
			enabled, enabledLoaded := r.enabled[cfg.ProviderID]
			r.mu.RUnlock()

			if !cfgLoaded || !enabledLoaded {
				if err := r.loadFromDB(); err != nil {
					return err
				}
				r.mu.RLock()
				existingCfg = r.configs[cfg.ProviderID]
				enabled = r.enabled[cfg.ProviderID]
				r.mu.RUnlock()
			}

			merged := mergeBuiltinConfig(existingCfg, cfg)
			if !cliProviderConfigsEqual(existingCfg, merged) {
				if err := r.upsertConfig(merged, enabled); err != nil {
					return fmt.Errorf("sync builtin %s: %w", cfg.ProviderID, err)
				}
			}
		}
	}
	return nil
}

func mergeBuiltinConfig(existing, builtin CLIProviderConfig) CLIProviderConfig {
	merged := existing

	if merged.ProviderID == "" {
		merged.ProviderID = builtin.ProviderID
	}
	if merged.Name == "" {
		merged.Name = builtin.Name
	}
	if merged.Binary == "" {
		merged.Binary = builtin.Binary
	}
	if len(merged.BaseArgs) == 0 && len(builtin.BaseArgs) > 0 {
		merged.BaseArgs = slices.Clone(builtin.BaseArgs)
	}
	if merged.ParserType == "" {
		merged.ParserType = builtin.ParserType
	}
	if !merged.SupportsResume && builtin.SupportsResume {
		merged.SupportsResume = true
	}
	if merged.ResumeFlag == "" {
		merged.ResumeFlag = builtin.ResumeFlag
	}
	if merged.ModelFlag == "" {
		merged.ModelFlag = builtin.ModelFlag
	}
	if merged.EffortFlag == "" {
		merged.EffortFlag = builtin.EffortFlag
	}
	if merged.SubAgentFlag == "" {
		merged.SubAgentFlag = builtin.SubAgentFlag
	}
	if merged.MCPMode == "" {
		merged.MCPMode = builtin.MCPMode
	}
	if len(merged.EnvVars) == 0 && len(builtin.EnvVars) > 0 {
		merged.EnvVars = cloneStringMap(builtin.EnvVars)
	}
	if merged.DefaultModelID == "" {
		merged.DefaultModelID = builtin.DefaultModelID
	}
	if merged.AttachmentMode == "" {
		merged.AttachmentMode = builtin.AttachmentMode
	}
	if merged.AttachmentFlag == "" {
		merged.AttachmentFlag = builtin.AttachmentFlag
	}
	if len(merged.Models) == 0 && len(builtin.Models) > 0 {
		merged.Models = slices.Clone(builtin.Models)
	}
	if len(merged.Efforts) == 0 && len(builtin.Efforts) > 0 {
		merged.Efforts = slices.Clone(builtin.Efforts)
	}
	if len(merged.SubAgents) == 0 && len(builtin.SubAgents) > 0 {
		merged.SubAgents = slices.Clone(builtin.SubAgents)
	}
	if merged.ModelDiscovery == nil && builtin.ModelDiscovery != nil {
		merged.ModelDiscovery = cloneModelDiscoveryConfig(builtin.ModelDiscovery)
	}

	return merged
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneModelDiscoveryConfig(src *ModelDiscoveryConfig) *ModelDiscoveryConfig {
	if src == nil {
		return nil
	}
	clone := *src
	clone.Command = slices.Clone(src.Command)
	clone.Filter = slices.Clone(src.Filter)
	clone.APIHeaders = cloneStringMap(src.APIHeaders)
	if src.Aliases != nil {
		clone.Aliases = append([]ModelInfo(nil), src.Aliases...)
	}
	return &clone
}

func cliProviderConfigsEqual(a, b CLIProviderConfig) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aj) == string(bj)
}

// GetConfig returns the CLIProviderConfig for a provider by ID.
func (r *ProviderRegistry) GetConfig(id string) (CLIProviderConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.configs[id]
	return cfg, ok
}

// GetCLIProvider returns the underlying CLIProvider for direct command building.
func (r *ProviderRegistry) GetCLIProvider(id string) (*CLIProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	if !ok {
		return nil, false
	}
	cli, ok := p.(*CLIProvider)
	return cli, ok
}
