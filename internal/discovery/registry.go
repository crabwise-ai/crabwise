package discovery

import (
	"sync"
)

type Registry struct {
	mu     sync.RWMutex
	agents map[string]*AgentInfo
}

func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]*AgentInfo),
	}
}

// Update replaces the registry with fresh scan results.
func (r *Registry) Update(agents []AgentInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Mark all as inactive first
	for _, a := range r.agents {
		a.Status = "inactive"
	}

	for i := range agents {
		a := agents[i]
		if existing, ok := r.agents[a.ID]; ok {
			existing.Status = a.Status
			existing.PID = a.PID
		} else {
			r.agents[a.ID] = &a
		}
	}
}

func (r *Registry) List() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]AgentInfo, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, *a)
	}
	return result
}

func (r *Registry) Get(id string) (*AgentInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.agents[id]
	if !ok {
		return nil, false
	}
	copy := *a
	return &copy, true
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}
