package proxy

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/openclawstate"
)

func TestBuildAuditEvent_UsesOpenClawMatch(t *testing.T) {
	p := &Proxy{}
	p.SetRequestAttributor(fakeAttributor{
		ok: true,
		result: openclawstate.MatchResult{
			AgentID:       "openclaw",
			SessionKey:    "agent:main:discord:channel:123",
			ParentSession: "agent:parent:discord:channel:456",
			RunID:         "run-1",
			Model:         "claude-sonnet",
			ThinkingLevel: "high",
		},
	})

	evt := p.buildAuditEvent("req-1", time.Now(), "anthropic", NormalizedRequest{Model: "claude-sonnet"}, "/v1/messages")
	if evt.AgentID != "openclaw" {
		t.Fatalf("expected agent_id openclaw, got %q", evt.AgentID)
	}
	if evt.SessionID != "agent:main:discord:channel:123" {
		t.Fatalf("expected session id from openclaw match, got %q", evt.SessionID)
	}
	if evt.ParentSessionID != "agent:parent:discord:channel:456" {
		t.Fatalf("expected parent session id, got %q", evt.ParentSessionID)
	}
	if evt.AdapterID != "proxy" || evt.AdapterType != "proxy" {
		t.Fatalf("expected proxy adapter identity, got %q/%q", evt.AdapterID, evt.AdapterType)
	}
	if !strings.Contains(evt.Arguments, "\"openclaw.run_id\":\"run-1\"") {
		t.Fatalf("expected openclaw metadata in arguments, got %q", evt.Arguments)
	}
	if !strings.Contains(evt.Arguments, "\"openclaw.correlation_confidence\":\"high\"") {
		t.Fatalf("expected high correlation confidence in arguments, got %q", evt.Arguments)
	}
}

func TestBuildAuditEvent_AmbiguousOpenClawMatchLeavesSessionEmpty(t *testing.T) {
	p := &Proxy{}
	p.SetRequestAttributor(fakeAttributor{
		ok: true,
		result: openclawstate.MatchResult{
			AgentID: "openclaw",
			Model:   "claude-sonnet",
		},
	})

	evt := p.buildAuditEvent("req-1", time.Now(), "anthropic", NormalizedRequest{Model: "claude-sonnet"}, "/v1/messages")
	if evt.AgentID != "openclaw" {
		t.Fatalf("expected agent_id openclaw, got %q", evt.AgentID)
	}
	if evt.SessionID != "" {
		t.Fatalf("expected empty session id for ambiguous match, got %q", evt.SessionID)
	}
	if !strings.Contains(evt.Arguments, "\"openclaw.correlation_confidence\":\"low\"") {
		t.Fatalf("expected low correlation confidence in arguments, got %q", evt.Arguments)
	}
}

func TestProxyBlockWithOpenClawAttribution(t *testing.T) {
	events := make(chan *audit.AuditEvent, 10)
	var upstreamHits int

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	cfg := testProxyConfig(upstreamURL, caCert, caKey)

	addr := freePort(t)
	cfg.Listen = addr

	p, err := New(cfg, blockEval{}, nil, events)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	p.SetRequestAttributor(fakeAttributor{
		ok: true,
		result: openclawstate.MatchResult{
			AgentID:    "openclaw",
			SessionKey: "agent:main:discord:channel:123",
			RunID:      "run-1",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = p.Stop()
	})

	go p.Start(ctx)
	waitForProxyHealth(t, addr)

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	u, _ := url.Parse(upstreamURL)
	target := "https://" + u.Host + "/v1/chat/completions"
	resp, err := client.Post(target, "application/json", strings.NewReader(`{"model":"claude-sonnet","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected blocked status 403, got %d", resp.StatusCode)
	}
	if upstreamHits != 0 {
		t.Fatalf("expected blocked request to never reach upstream, got %d hits", upstreamHits)
	}

	select {
	case evt := <-events:
		if evt.AgentID != "openclaw" {
			t.Fatalf("expected event agent_id openclaw, got %q", evt.AgentID)
		}
		if evt.SessionID != "agent:main:discord:channel:123" {
			t.Fatalf("expected session id from openclaw attribution, got %q", evt.SessionID)
		}
		if evt.Outcome != audit.OutcomeBlocked {
			t.Fatalf("expected blocked outcome, got %q", evt.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked proxy event")
	}
}

type fakeAttributor struct {
	result openclawstate.MatchResult
	ok     bool
}

func (f fakeAttributor) MatchProxyRequest(time.Time, string, string) (openclawstate.MatchResult, bool) {
	return f.result, f.ok
}

func waitForProxyHealth(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("proxy at %s not ready", addr)
}
