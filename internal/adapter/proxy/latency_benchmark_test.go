package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"math"
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
		drainAndClose(resp.Body)
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
			drainAndClose(resp.Body)
			t.Fatalf("sample request %d unexpected status: %d", i+1, resp.StatusCode)
		}
		drainAndClose(resp.Body)
		samples = append(samples, time.Since(start))
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p50 := samples[percentileIndex(len(samples), 50)]
	p95 := samples[percentileIndex(len(samples), 95)]
	p99 := samples[percentileIndex(len(samples), 99)]
	max := samples[len(samples)-1]

	t.Logf("m3_bench proxy_roundtrip p50=%s p95=%s p99=%s max=%s", p50, p95, p99, max)
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
	caCert, caKey, _ := generateTestCA(t)
	proxyAddr := startTestProxy(t, testProxyConfig(upstreamURL, caCert, caKey), allowEval{})

	requestBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"first token gate"}]}`

	directClient := &http.Client{Timeout: 5 * time.Second}
	directTarget := upstreamURL + "/v1/chat/completions"

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	proxyClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	u, _ := url.Parse(upstreamURL)
	proxyTarget := "http://" + u.Host + "/v1/chat/completions"

	const warmupRequests = 5
	for i := 0; i < warmupRequests; i++ {
		if _, err := firstTokenLatency(directClient, directTarget, requestBody); err != nil {
			t.Fatalf("warmup direct first-token sample %d failed: %v", i+1, err)
		}
		if _, err := firstTokenLatency(proxyClient, proxyTarget, requestBody); err != nil {
			t.Fatalf("warmup proxy first-token sample %d failed: %v", i+1, err)
		}
	}

	const sampleCount = 25
	directSamples := make([]time.Duration, 0, sampleCount)
	proxySamples := make([]time.Duration, 0, sampleCount)
	deltaSamples := make([]time.Duration, 0, sampleCount)

	for i := 0; i < sampleCount; i++ {
		directFirstToken, err := firstTokenLatency(directClient, directTarget, requestBody)
		if err != nil {
			t.Fatalf("direct first-token sample %d failed: %v", i+1, err)
		}
		proxyFirstToken, err := firstTokenLatency(proxyClient, proxyTarget, requestBody)
		if err != nil {
			t.Fatalf("proxy first-token sample %d failed: %v", i+1, err)
		}

		directSamples = append(directSamples, directFirstToken)
		proxySamples = append(proxySamples, proxyFirstToken)
		deltaSamples = append(deltaSamples, proxyFirstToken-directFirstToken)
	}

	directP50 := percentileDuration(directSamples, 50)
	proxyP50 := percentileDuration(proxySamples, 50)
	deltaP50 := percentileDuration(deltaSamples, 50)
	deltaP95 := percentileDuration(deltaSamples, 95)
	deltaMax := percentileDuration(deltaSamples, 100)

	t.Logf("m3_bench proxy_first_token p50=%s p95=%s max=%s", deltaP50, deltaP95, deltaMax)
	if deltaP95 >= 50*time.Millisecond {
		t.Fatalf("first token p95 delta too high: %s (direct_p50=%s proxy_p50=%s)", deltaP95, directP50, proxyP50)
	}
}

func firstTokenLatency(client *http.Client, targetURL, requestBody string) (time.Duration, error) {
	start := time.Now()
	resp, err := client.Post(targetURL, "application/json", strings.NewReader(requestBody))
	if err != nil {
		return 0, err
	}
	defer drainAndClose(resp.Body)

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

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func percentileDuration(samples []time.Duration, percentile int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[percentileIndex(len(sorted), percentile)]
}

func percentileIndex(sampleCount, percentile int) int {
	if sampleCount <= 0 {
		return 0
	}
	idx := int(math.Ceil(float64(sampleCount*percentile)/100.0)) - 1
	if idx < 0 {
		return 0
	}
	if idx >= sampleCount {
		return sampleCount - 1
	}
	return idx
}
