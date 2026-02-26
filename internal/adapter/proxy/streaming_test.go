package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// unit tests for parseSSEData / parseSSEEventType
// ---------------------------------------------------------------------------

func TestParseSSEData(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    string
		wantNil bool
	}{
		{"normal data line", "data: {\"id\":\"1\"}\n", `{"id":"1"}`, false},
		{"data with extra spaces", "data:   hello  \n", "hello", false},
		{"empty line", "\n", "", true},
		{"comment line", ": keep-alive\n", "", true},
		{"non-data field", "event: message\n", "", true},
		{"done sentinel", "data: [DONE]\n", "[DONE]", false},
		{"empty data value", "data: \n", "", false},
		{"data no space after colon", "data:nospace\n", "nospace", false},
		{"data with only whitespace", "   \n", "", true},
		{"data colon only", "data:\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSSEData([]byte(tt.line))
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %q, got nil", tt.want)
			}
			if string(got) != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, string(got))
			}
		})
	}
}

func TestParseSSEEventType(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"event line", "event: message\n", "message"},
		{"event with spaces", "event:   response.done  \n", "response.done"},
		{"not an event", "data: something\n", ""},
		{"empty line", "\n", ""},
		{"comment line", ": keep-alive\n", ""},
		{"event no space after colon", "event:delta\n", "delta"},
		{"event colon only", "event:\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSSEEventType([]byte(tt.line))
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// torture suite helpers
// ---------------------------------------------------------------------------

// mockStreamTransport implements Transport for testing proxySSEStream.
// Only ParseStreamEvent is meaningful; PrepareAuth and Forward are stubs.
type mockStreamTransport struct {
	mu     sync.Mutex
	events []StreamEvent
	idx    int
}

func (m *mockStreamTransport) PrepareAuth(_ *http.Request) error { return nil }
func (m *mockStreamTransport) Forward(_ context.Context, _ *http.Request) (*http.Response, error) {
	return nil, nil
}
func (m *mockStreamTransport) ParseStreamEvent(_ []byte) (StreamEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx < len(m.events) {
		e := m.events[m.idx]
		m.idx++
		return e, nil
	}
	return StreamEvent{}, nil
}

// sseRecorder wraps httptest.ResponseRecorder with http.Flusher support.
type sseRecorder struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	flushed int
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *sseRecorder) Flush() {
	r.mu.Lock()
	r.flushed++
	r.mu.Unlock()
}

// errorAfterNWriter returns an error after N successful writes.
type errorAfterNWriter struct {
	inner    http.ResponseWriter
	maxWrite int
	writes   int
	mu       sync.Mutex
}

func (w *errorAfterNWriter) Header() http.Header       { return w.inner.Header() }
func (w *errorAfterNWriter) WriteHeader(statusCode int) { w.inner.WriteHeader(statusCode) }
func (w *errorAfterNWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	if w.writes > w.maxWrite {
		return 0, errors.New("client disconnected")
	}
	return w.inner.Write(data)
}

// malformedJSONTransport always returns parse error.
type malformedJSONTransport struct{}

func (m *malformedJSONTransport) PrepareAuth(_ *http.Request) error { return nil }
func (m *malformedJSONTransport) Forward(_ context.Context, _ *http.Request) (*http.Response, error) {
	return nil, nil
}
func (m *malformedJSONTransport) ParseStreamEvent(_ []byte) (StreamEvent, error) {
	return StreamEvent{}, errors.New("invalid JSON")
}

// eventTypeCapture is a Transport stub for event type propagation tests.
type eventTypeCapture struct{}

func (e *eventTypeCapture) PrepareAuth(_ *http.Request) error { return nil }
func (e *eventTypeCapture) Forward(_ context.Context, _ *http.Request) (*http.Response, error) {
	return nil, nil
}
func (e *eventTypeCapture) ParseStreamEvent(_ []byte) (StreamEvent, error) {
	return StreamEvent{Model: "gpt-4o"}, nil
}

// ---------------------------------------------------------------------------
// torture test cases
// ---------------------------------------------------------------------------

func TestProxySSEStream_NormalFlow(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{
			{Model: "gpt-4o"},
			{Model: "gpt-4o"},
			{Model: "gpt-4o", HasFinish: true, FinishReason: "stop", HasUsage: true, InputTokens: 10, OutputTokens: 5},
		},
	}

	pr, pw := io.Pipe()
	rec := newSSERecorder()

	done := make(chan struct{})
	var tel streamTelemetry
	var sErr error
	go func() {
		tel, sErr = proxySSEStream(rec, pr, tr, 5*time.Second)
		close(done)
	}()

	for _, line := range []string{
		"data: {\"chunk\":1}\n", "\n",
		"data: {\"chunk\":2}\n", "\n",
		"data: {\"chunk\":3}\n", "\n",
		"data: [DONE]\n", "\n",
	} {
		if _, err := pw.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("proxySSEStream error: %v", sErr)
	}
	if tel.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %q", tel.Model)
	}
	if tel.FinishReason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", tel.FinishReason)
	}
	if tel.InputTokens != 10 || tel.OutputTokens != 5 {
		t.Fatalf("expected tokens 10/5, got %d/%d", tel.InputTokens, tel.OutputTokens)
	}
	if tel.FirstTokenAt.IsZero() {
		t.Fatal("expected FirstTokenAt to be set")
	}
	if !strings.Contains(rec.Body.String(), "chunk") {
		t.Fatalf("expected recorder to contain chunk data, got %q", rec.Body.String())
	}
}

func TestProxySSEStream_PartialChunks(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{{Model: "gpt-4o"}},
	}

	pr, pw := io.Pipe()
	rec := newSSERecorder()

	done := make(chan struct{})
	var tel streamTelemetry
	var sErr error
	go func() {
		tel, sErr = proxySSEStream(rec, pr, tr, 5*time.Second)
		close(done)
	}()

	pw.Write([]byte("data: {\"id\":"))
	time.Sleep(50 * time.Millisecond)
	pw.Write([]byte("\"1\"}\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("proxySSEStream error: %v", sErr)
	}
	if tel.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %q", tel.Model)
	}
}

func TestProxySSEStream_MultiEventChunk(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{
			{Model: "gpt-4o"},
			{Model: "gpt-4o", HasFinish: true, FinishReason: "stop"},
		},
	}

	pr, pw := io.Pipe()
	rec := newSSERecorder()

	done := make(chan struct{})
	var tel streamTelemetry
	var sErr error
	go func() {
		tel, sErr = proxySSEStream(rec, pr, tr, 5*time.Second)
		close(done)
	}()

	pw.Write([]byte("data: {\"a\":1}\ndata: {\"b\":2}\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("proxySSEStream error: %v", sErr)
	}
	if tel.FinishReason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", tel.FinishReason)
	}
}

func TestProxySSEStream_EmptyKeepAlive(t *testing.T) {
	tr := &mockStreamTransport{}

	pr, pw := io.Pipe()
	rec := newSSERecorder()

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = proxySSEStream(rec, pr, tr, 5*time.Second)
		close(done)
	}()

	pw.Write([]byte(": keep-alive\n"))
	pw.Write([]byte("\n"))
	pw.Write([]byte(": keep-alive\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("proxySSEStream error: %v", sErr)
	}
	if !strings.Contains(rec.Body.String(), "keep-alive") {
		t.Fatal("expected keep-alive comments to be passed through")
	}
}

func TestProxySSEStream_MalformedJSON(t *testing.T) {
	tr := &malformedJSONTransport{}

	pr, pw := io.Pipe()
	rec := newSSERecorder()

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = proxySSEStream(rec, pr, tr, 5*time.Second)
		close(done)
	}()

	pw.Write([]byte("data: {bad json}\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("expected no error for malformed JSON, got: %v", sErr)
	}
}

func TestProxySSEStream_UpstreamTimeout(t *testing.T) {
	tr := &mockStreamTransport{}

	pr, _ := io.Pipe() // never write, never close
	rec := newSSERecorder()

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = proxySSEStream(rec, pr, tr, 100*time.Millisecond)
		close(done)
	}()

	<-done

	if sErr == nil {
		t.Fatal("expected idle timeout error")
	}
	if !strings.Contains(sErr.Error(), "idle timeout") {
		t.Fatalf("expected idle timeout error, got: %v", sErr)
	}
}

func TestProxySSEStream_ClientDisconnect(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{{Model: "gpt-4o"}, {Model: "gpt-4o"}},
	}

	pr, pw := io.Pipe()
	errWriter := &errorAfterNWriter{
		inner:    newSSERecorder(),
		maxWrite: 1, // fail on second write
	}

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = proxySSEStream(errWriter, pr, tr, 5*time.Second)
		close(done)
	}()

	pw.Write([]byte("data: {\"a\":1}\n"))
	time.Sleep(20 * time.Millisecond)
	pw.Write([]byte("data: {\"b\":2}\n"))
	pw.Close()
	<-done

	if sErr == nil {
		t.Fatal("expected write error from client disconnect")
	}
	if !strings.Contains(sErr.Error(), "client disconnected") {
		t.Fatalf("expected client disconnected error, got: %v", sErr)
	}
}

func TestProxySSEStream_EventTypeField(t *testing.T) {
	tr := &eventTypeCapture{}

	pr, pw := io.Pipe()
	rec := newSSERecorder()

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = proxySSEStream(rec, pr, tr, 5*time.Second)
		close(done)
	}()

	pw.Write([]byte("event: response.done\n"))
	pw.Write([]byte("data: {\"final\":true}\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("proxySSEStream error: %v", sErr)
	}
	if !strings.Contains(rec.Body.String(), "event: response.done") {
		t.Fatal("expected event type line to be written to client")
	}
}
