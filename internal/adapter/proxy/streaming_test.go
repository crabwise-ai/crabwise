package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
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

// mockStreamTransport implements Transport for testing bufferSSEStream.
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
func (m *mockStreamTransport) ExtractToolUseBlocks(_ []byte) ([]ToolUseBlock, error) {
	return nil, nil
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
func (m *malformedJSONTransport) ExtractToolUseBlocks(_ []byte) ([]ToolUseBlock, error) {
	return nil, nil
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
func (e *eventTypeCapture) ExtractToolUseBlocks(_ []byte) ([]ToolUseBlock, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// torture test cases
// ---------------------------------------------------------------------------

func TestBufferSSEStream_NormalFlow(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{
			{Model: "gpt-4o"},
			{Model: "gpt-4o"},
			{Model: "gpt-4o", HasFinish: true, FinishReason: "stop", HasUsage: true, InputTokens: 10, OutputTokens: 5},
		},
	}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var result bufferedSSEResult
	var sErr error
	go func() {
		result, sErr = bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
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
		t.Fatalf("bufferSSEStream error: %v", sErr)
	}
	tel := result.Telemetry
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
	if !strings.Contains(string(result.Body), "chunk") {
		t.Fatalf("expected body to contain chunk data, got %q", string(result.Body))
	}
}

func TestBufferSSEStream_PartialChunks(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{{Model: "gpt-4o"}},
	}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var result bufferedSSEResult
	var sErr error
	go func() {
		result, sErr = bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
		close(done)
	}()

	pw.Write([]byte("data: {\"id\":"))
	time.Sleep(50 * time.Millisecond)
	pw.Write([]byte("\"1\"}\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("bufferSSEStream error: %v", sErr)
	}
	if result.Telemetry.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %q", result.Telemetry.Model)
	}
}

func TestBufferSSEStream_MultiEventChunk(t *testing.T) {
	tr := &mockStreamTransport{
		events: []StreamEvent{
			{Model: "gpt-4o"},
			{Model: "gpt-4o", HasFinish: true, FinishReason: "stop"},
		},
	}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var result bufferedSSEResult
	var sErr error
	go func() {
		result, sErr = bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
		close(done)
	}()

	pw.Write([]byte("data: {\"a\":1}\ndata: {\"b\":2}\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("bufferSSEStream error: %v", sErr)
	}
	if result.Telemetry.FinishReason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", result.Telemetry.FinishReason)
	}
}

func TestBufferSSEStream_EmptyKeepAlive(t *testing.T) {
	tr := &mockStreamTransport{}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var result bufferedSSEResult
	var sErr error
	go func() {
		result, sErr = bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
		close(done)
	}()

	pw.Write([]byte(": keep-alive\n"))
	pw.Write([]byte("\n"))
	pw.Write([]byte(": keep-alive\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("bufferSSEStream error: %v", sErr)
	}
	if !strings.Contains(string(result.Body), "keep-alive") {
		t.Fatal("expected keep-alive comments to be buffered")
	}
}

func TestBufferSSEStream_MalformedJSON(t *testing.T) {
	// bufferSSEStream fails closed on parse errors — expect an error.
	tr := &malformedJSONTransport{}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
		close(done)
	}()

	pw.Write([]byte("data: {bad json}\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr == nil {
		t.Fatal("expected enforcement_error for malformed JSON (fail-closed), got nil")
	}
	if !strings.Contains(sErr.Error(), "enforcement_error") {
		t.Fatalf("expected enforcement_error, got: %v", sErr)
	}
}

func TestBufferSSEStream_UpstreamTimeout(t *testing.T) {
	tr := &mockStreamTransport{}

	pr, _ := io.Pipe() // never write, never close

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = bufferSSEStream(pr, tr, 100*time.Millisecond, defaultStreamMaxBytes)
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

func TestBufferSSEStream_BufferOverflow(t *testing.T) {
	tr := &mockStreamTransport{}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var sErr error
	go func() {
		_, sErr = bufferSSEStream(pr, tr, 5*time.Second, 10) // tiny 10-byte limit
		close(done)
	}()

	pw.Write([]byte("data: {\"a\":1}\n")) // 14 bytes > 10
	pw.Close()
	<-done

	if sErr == nil {
		t.Fatal("expected buffer overflow error")
	}
	if !strings.Contains(sErr.Error(), "stream buffer exceeded") {
		t.Fatalf("expected stream buffer exceeded error, got: %v", sErr)
	}
}

func TestBufferSSEStream_EventTypeField(t *testing.T) {
	tr := &eventTypeCapture{}

	pr, pw := io.Pipe()

	done := make(chan struct{})
	var result bufferedSSEResult
	var sErr error
	go func() {
		result, sErr = bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
		close(done)
	}()

	pw.Write([]byte("event: response.done\n"))
	pw.Write([]byte("data: {\"final\":true}\n"))
	pw.Write([]byte("\n"))
	pw.Close()
	<-done

	if sErr != nil {
		t.Fatalf("bufferSSEStream error: %v", sErr)
	}
	if !strings.Contains(string(result.Body), "event: response.done") {
		t.Fatal("expected event type line to be buffered")
	}
}
