//go:build m3_bench

package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxySustainedLoad(t *testing.T) {
	t.Log("m3_bench_profile sustained_load")

	// 16KB response body
	respBody := strings.Repeat(`{"id":"bench","model":"gpt-4o","choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":100}}`, 100)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	proxyAddr := startTestProxy(t, testProxyConfig(upstreamURL, caCert, caKey), allowEval{})

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	u, _ := url.Parse(upstreamURL)
	target := "https://" + u.Host + "/v1/chat/completions"

	// 4KB request body
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"` + strings.Repeat("a", 3800) + `"}]}`

	const (
		concurrency = 10
		warmupDur   = 5 * time.Second
		measureDur  = 15 * time.Second
	)

	makeClient := func() *http.Client {
		return &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{RootCAs: caPool},
				MaxIdleConns:    20,
			},
		}
	}

	// Warmup
	t.Log("warmup phase...")
	warmupDone := time.After(warmupDur)
	var wg sync.WaitGroup
	stopWarmup := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := makeClient()
			for {
				select {
				case <-stopWarmup:
					return
				default:
				}
				resp, err := client.Post(target, "application/json", strings.NewReader(reqBody))
				if err != nil {
					continue
				}
				drainAndClose(resp.Body)
			}
		}()
	}
	<-warmupDone
	close(stopWarmup)
	wg.Wait()

	// Measurement phase
	t.Log("measurement phase...")
	var totalReqs atomic.Int64
	var totalErrors atomic.Int64
	var mu sync.Mutex
	var allLatencies []time.Duration

	measureDone := time.After(measureDur)
	stopMeasure := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := makeClient()
			var localLatencies []time.Duration
			for {
				select {
				case <-stopMeasure:
					mu.Lock()
					allLatencies = append(allLatencies, localLatencies...)
					mu.Unlock()
					return
				default:
				}
				start := time.Now()
				resp, err := client.Post(target, "application/json", strings.NewReader(reqBody))
				elapsed := time.Since(start)
				totalReqs.Add(1)
				if err != nil {
					totalErrors.Add(1)
					continue
				}
				if resp.StatusCode != http.StatusOK {
					totalErrors.Add(1)
				}
				drainAndClose(resp.Body)
				localLatencies = append(localLatencies, elapsed)
			}
		}()
	}
	<-measureDone
	close(stopMeasure)
	wg.Wait()

	// Memory measurement
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	sort.Slice(allLatencies, func(i, j int) bool { return allLatencies[i] < allLatencies[j] })

	reqs := totalReqs.Load()
	errs := totalErrors.Load()

	var p50, p95, p99, maxLat time.Duration
	if len(allLatencies) > 0 {
		p50 = allLatencies[percentileIndex(len(allLatencies), 50)]
		p95 = allLatencies[percentileIndex(len(allLatencies), 95)]
		p99 = allLatencies[percentileIndex(len(allLatencies), 99)]
		maxLat = allLatencies[len(allLatencies)-1]
	}

	t.Logf("m3_bench sustained_load total_requests=%d errors=%d", reqs, errs)
	t.Logf("m3_bench sustained_load p50=%s p95=%s p99=%s max=%s", p50, p95, p99, maxLat)
	t.Logf("m3_bench sustained_load rps=%.0f", float64(reqs)/measureDur.Seconds())
	t.Logf("m3_bench sustained_load go_memory_sys=%dMB heap_alloc=%dMB",
		mem.Sys/1024/1024, mem.HeapAlloc/1024/1024)

	// Gate: Go memory footprint < 80MB
	maxGoMemory := uint64(80 * 1024 * 1024) // 80MB
	if mem.Sys > maxGoMemory {
		t.Fatalf("Go memory footprint too high: %dMB (limit 80MB)", mem.Sys/1024/1024)
	}
}
