package proxy

import (
	"fmt"
	"net/http"
	"path"
	"strings"
)

const providerHeader = "X-Crabwise-Provider"

type Router struct {
	defaultProvider string
	providers       map[string]*ProviderRuntime
}

func NewRouter(defaultProvider string, providers map[string]*ProviderRuntime) (*Router, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("router requires at least one provider")
	}
	if defaultProvider == "" {
		return nil, fmt.Errorf("default provider required")
	}
	if _, ok := providers[defaultProvider]; !ok {
		return nil, fmt.Errorf("default provider %q not registered", defaultProvider)
	}
	return &Router{defaultProvider: defaultProvider, providers: providers}, nil
}

func (r *Router) Resolve(req *http.Request) (*ProviderRuntime, string, error) {
	if req == nil || req.URL == nil {
		return nil, "", fmt.Errorf("invalid request")
	}

	if explicit := strings.TrimSpace(strings.ToLower(req.Header.Get(providerHeader))); explicit != "" {
		if rt, ok := r.providers[explicit]; ok {
			return rt, explicit, nil
		}
		return nil, explicit, fmt.Errorf("unknown provider %q", explicit)
	}

	requestPath := req.URL.Path
	for name, runtime := range r.providers {
		for _, pattern := range runtime.Config.RoutePatterns {
			if routeMatches(pattern, requestPath) {
				return runtime, name, nil
			}
		}
	}

	if rt, ok := r.providers[r.defaultProvider]; ok {
		return rt, r.defaultProvider, nil
	}
	return nil, "", fmt.Errorf("no route and default provider unavailable")
}

func routeMatches(pattern, requestPath string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(requestPath, strings.TrimSuffix(pattern, "*"))
	}
	if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
		ok, err := path.Match(pattern, requestPath)
		return err == nil && ok
	}
	return requestPath == pattern
}
