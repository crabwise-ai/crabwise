package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestProxyLatencyGate(t *testing.T) {
	t.Log("m3_bench_profile ci_reduced")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"bench","model":"gpt-4o","choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	proxyAddr := startTestProxy(t, testProxyConfig(upstreamURL, caCert, caKey), allowEval{})

	proxyURL, _ := url.Parse("http://" + proxyAddr)
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
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"latency gate"}]}`

	const warmupRequests = 5
	for i := 0; i < warmupRequests; i++ {
		resp, err := client.Post(target, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("warmup request %d failed: %v", i+1, err)
		}
		_ = resp.Body.Close()
	}

	const sampleCount = 40
	samples := make([]time.Duration, 0, sampleCount)
	for i := 0; i < sampleCount; i++ {
		start := time.Now()
		resp, err := client.Post(target, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("sample request %d failed: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			t.Fatalf("sample request %d unexpected status: %d", i+1, resp.StatusCode)
		}
		_ = resp.Body.Close()
		samples = append(samples, time.Since(start))
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[percentileIndex(len(samples), 95)]
	p99 := samples[percentileIndex(len(samples), 99)]

	t.Logf("m3_bench proxy_roundtrip p95=%s p99=%s", p95, p99)
	if p95 >= 20*time.Millisecond {
		t.Fatalf("p95 too high: %s", p95)
	}
}

func TestProxyFirstTokenGate(t *testing.T) {
	t.Log("m3_bench_profile ci_reduced")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		time.Sleep(8 * time.Millisecond)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	proxyAddr := startTestProxy(t, testProxyConfig(upstreamURL, caCert, caKey), allowEval{})

	requestBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"first token gate"}]}`

	directClient := &http.Client{Timeout: 5 * time.Second}
	directTarget := upstreamURL + "/v1/chat/completions"
	directFirstToken, err := firstTokenLatency(directClient, directTarget, requestBody)
	if err != nil {
		t.Fatalf("measure direct first token latency: %v", err)
	}

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	proxyClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}
	u, _ := url.Parse(upstreamURL)
	proxyTarget := "https://" + u.Host + "/v1/chat/completions"
	proxyFirstToken, err := firstTokenLatency(proxyClient, proxyTarget, requestBody)
	if err != nil {
		t.Fatalf("measure proxy first token latency: %v", err)
	}

	delta := proxyFirstToken - directFirstToken
	if delta < 0 {
		delta = 0
	}

	t.Logf("m3_bench proxy_first_token delta=%s", delta)
	if delta >= 50*time.Millisecond {
		t.Fatalf("first token delta too high: %s", delta)
	}
}

func firstTokenLatency(client *http.Client, targetURL, requestBody string) (time.Duration, error) {
	start := time.Now()
	resp, err := client.Post(targetURL, "application/json", strings.NewReader(requestBody))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		if strings.HasPrefix(line, "data: ") {
			return time.Since(start), nil
		}
	}
}

func percentileIndex(sampleCount, percentile int) int {
	idx := (sampleCount*percentile)/100 - 1
	if idx < 0 {
		return 0
	}
	if idx >= sampleCount {
		return sampleCount - 1
	}
	return idx
}
