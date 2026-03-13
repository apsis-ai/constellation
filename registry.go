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
	mu     sync.RWMutex
	agents []AgentInfo
}

// NewRegistry creates a registry pre-populated with the default agents.
func NewRegistry() *Registry {
	return &Registry{
		agents: []AgentInfo{
			{ID: "claude", Name: "Claude", Available: true, DefaultModel: "sonnet"},
			{ID: "codex", Name: "Codex", Available: true, DefaultModel: "o4-mini"},
			{ID: "opencode", Name: "OpenCode", Available: true},
			{ID: "cursor", Name: "Cursor", Available: true},
		},
	}
}

// ListAgents returns all registered agents.
func (r *Registry) ListAgents() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentInfo, len(r.agents))
	copy(out, r.agents)
	return out
}

// GetAgent returns the agent with the given ID, if found.
func (r *Registry) GetAgent(id string) (AgentInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.agents {
		if a.ID == id {
			return a, true
		}
	}
	return AgentInfo{}, false
}

// Register adds or updates an agent in the registry.
func (r *Registry) Register(info AgentInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, a := range r.agents {
		if a.ID == info.ID {
			r.agents[i] = info
			return
		}
	}
	r.agents = append(r.agents, info)
}

// Discover runs CLI discovery for each agent to check availability.
// This checks if the binary is on PATH and updates the Available field.
func (r *Registry) Discover() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.agents {
		_, err := resolveAgentBinary(r.agents[i].ID)
		r.agents[i].Available = err == nil
	}
}
