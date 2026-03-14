package mux

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	now := nowUnix()
	_, err = r.db.Exec(`INSERT INTO providers (id, name, type, parser_type, enabled, config, created_at, updated_at)
		VALUES (?, ?, 'cli', ?, 1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=?, parser_type=?, config=?, updated_at=?`,
		cfg.ProviderID, cfg.Name, cfg.ParserType, string(configJSON), now, now,
		cfg.Name, cfg.ParserType, string(configJSON), now)
	if err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[cfg.ProviderID] = cfg
	r.enabled[cfg.ProviderID] = true
	r.providers[cfg.ProviderID] = NewCLIProvider(cfg, r.parsers)
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
			// Load existing config into cache if not already loaded
			r.mu.RLock()
			_, loaded := r.providers[cfg.ProviderID]
			r.mu.RUnlock()
			if !loaded {
				if err := r.loadFromDB(); err != nil {
					return err
				}
			}
		}
	}
	return nil
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
