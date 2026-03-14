package mux

import "sync"

// AgentInfo describes a registered agent and its capabilities.
type AgentInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Available    bool     `json:"available"`
	Models       []string `json:"models,omitempty"`
	SubAgents    []string `json:"sub_agents,omitempty"`
	DefaultModel string   `json:"default_model,omitempty"`
}

// Registry holds known agents and supports static + dynamic registration.
type Registry struct {
	mu        sync.RWMutex
	providers *ProviderRegistry
	overrides map[string]AgentInfo // for agents registered directly
}

// NewRegistry creates a registry backed by a ProviderRegistry.
func NewRegistry(provReg *ProviderRegistry) *Registry {
	return &Registry{
		providers: provReg,
		overrides: make(map[string]AgentInfo),
	}
}

// ListAgents returns all registered agents.
func (r *Registry) ListAgents() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := r.providers.ListAll()
	agents := make([]AgentInfo, 0, len(all)+len(r.overrides))

	seen := make(map[string]bool)
	for _, info := range all {
		prov, ok := r.providers.Get(info.ID)
		available := false
		defaultModel := ""
		if ok {
			available = prov.Validate() == nil
			defaultModel = prov.DefaultModel()
		}
		agents = append(agents, AgentInfo{
			ID:           info.ID,
			Name:         info.Name,
			Available:    available,
			DefaultModel: defaultModel,
		})
		seen[info.ID] = true
	}
	for id, info := range r.overrides {
		if !seen[id] {
			agents = append(agents, info)
		}
	}
	return agents
}

// GetAgent returns the agent with the given ID, if found.
func (r *Registry) GetAgent(id string) (AgentInfo, bool) {
	r.mu.RLock()
	if info, ok := r.overrides[id]; ok {
		r.mu.RUnlock()
		return info, true
	}
	r.mu.RUnlock()

	all := r.providers.ListAll()
	for _, info := range all {
		if info.ID == id {
			prov, ok := r.providers.Get(id)
			available := false
			defaultModel := ""
			if ok {
				available = prov.Validate() == nil
				defaultModel = prov.DefaultModel()
			}
			return AgentInfo{
				ID:           info.ID,
				Name:         info.Name,
				Available:    available,
				DefaultModel: defaultModel,
			}, true
		}
	}
	return AgentInfo{}, false
}

// Register adds or updates an agent in the registry.
func (r *Registry) Register(info AgentInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[info.ID] = info
}

// Discover checks binary availability for all agents.
func (r *Registry) Discover() {
	// Provider-backed agents auto-discover via Validate()
	// No-op for the new implementation since ListAgents calls Validate() dynamically
}
