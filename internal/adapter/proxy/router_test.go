package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterResolvePrecedence(t *testing.T) {
	providers := map[string]*ProviderRuntime{
		"openai": {Name: "openai", Config: ProviderConfig{RoutePatterns: []string{"/v1/responses"}}},
		"other":  {Name: "other", Config: ProviderConfig{RoutePatterns: []string{"/v1/chat/*"}}},
	}
	r, err := NewRouter("openai", providers)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	req.Header.Set(providerHeader, "openai")
	got, _, err := r.Resolve(req)
	if err != nil {
		t.Fatalf("resolve header: %v", err)
	}
	if got.Name != "openai" {
		t.Fatalf("expected header provider openai, got %s", got.Name)
	}

	req = httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	got, _, err = r.Resolve(req)
	if err != nil {
		t.Fatalf("resolve route: %v", err)
	}
	if got.Name != "other" {
		t.Fatalf("expected route provider other, got %s", got.Name)
	}
}
