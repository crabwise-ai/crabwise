package discovery

import (
	"sync"
	"time"
)

type Registry struct {
	mu      sync.RWMutex
	agents  map[string]*AgentInfo
	sources map[string]map[string]struct{}
}

func NewRegistry() *Registry {
	return &Registry{
		agents:  make(map[string]*AgentInfo),
		sources: make(map[string]map[string]struct{}),
	}
}

// Update replaces the registry with fresh scan results.
func (r *Registry) Update(agents []AgentInfo) {
	r.ReplaceSource("scanner", agents)
}

func (r *Registry) ReplaceSource(source string, agents []AgentInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	prev := r.sources[source]
	next := make(map[string]struct{}, len(agents))

	for i := range agents {
		a := normalizeAgent(agents[i])
		next[a.ID] = struct{}{}
		copy := a
		r.agents[a.ID] = &copy
	}

	for id := range prev {
		if _, ok := next[id]; !ok {
			delete(r.agents, id)
		}
	}

	r.sources[source] = next
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

func normalizeAgent(a AgentInfo) AgentInfo {
	if a.DiscoveredAt.IsZero() {
		a.DiscoveredAt = time.Now().UTC()
	}
	if a.PID == 0 && !a.LastActivityAt.IsZero() {
		if time.Since(a.LastActivityAt) < 5*time.Minute {
			a.Status = "active"
		} else {
			a.Status = "inactive"
		}
	}
	return a
}
