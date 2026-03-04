package proxy

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/openclawstate"
)

type Pricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

type Config struct {
	Listen              string
	DefaultProvider     string
	UpstreamTimeout     time.Duration
	StreamIdleTimeout   time.Duration
	MaxRequestBody      int64
	RedactEgressDefault bool
	CACert              string
	CAKey               string
	MappingsDir         string
	MappingStrictMode   bool
	Providers           map[string]ProviderConfig
	Pricing             map[string]Pricing
	RedactPatterns      []string
}

type ProviderConfig struct {
	Name            string
	UpstreamBaseURL string
	AuthMode        string // passthrough|configured
	AuthKey         string // literal or env:VAR
	RoutePatterns   []string
	MaxIdleConns    int
	IdleConnTimeout time.Duration
}

type Transport interface {
	PrepareAuth(req *http.Request) error
	Forward(ctx context.Context, req *http.Request) (*http.Response, error)
	ParseStreamEvent(data []byte) (StreamEvent, error)
	// ExtractToolUseBlocks extracts complete tool_use blocks from a
	// non-streaming response body. Returns nil slice if none present.
	ExtractToolUseBlocks(body []byte) ([]ToolUseBlock, error)
}

type TransportFactory func(cfg ProviderConfig, upstreamTimeout time.Duration) Transport

var (
	transportsMu       sync.RWMutex
	transportFactories = map[string]TransportFactory{}
)

func RegisterTransport(name string, factory TransportFactory) {
	transportsMu.Lock()
	defer transportsMu.Unlock()
	transportFactories[strings.ToLower(name)] = factory
}

func lookupTransportFactory(name string) (TransportFactory, bool) {
	transportsMu.RLock()
	defer transportsMu.RUnlock()
	f, ok := transportFactories[strings.ToLower(name)]
	return f, ok
}

type StreamEvent struct {
	Model          string
	FinishReason   string
	InputTokens    int64
	OutputTokens   int64
	HasUsage       bool
	HasFinish      bool
	EventType      string         // SSE event: field value (e.g. "content_block_delta" for Anthropic)
	ToolCallDeltas []ToolCallDelta // NEW: streaming tool call fragments
}

type NormalizedTool struct {
	Name                 string
	RawArgs              []byte
	ArgKeys              []string
	Category             string
	Effect               string
	TaxonomyVersion      string
	ClassificationSource string
}

type NormalizedRequest struct {
	Provider        string
	Endpoint        string
	Model           string
	Stream          bool
	Tools           []NormalizedTool
	InputSummary    string
	MappingDegraded bool
}

type NormalizedResponse struct {
	Model           string
	FinishReason    string
	InputTokens     int64
	OutputTokens    int64
	UpstreamStatus  int
	ErrorType       string
	ErrorMessage    string
	MappingDegraded bool
}

type ProviderRuntime struct {
	Name      string
	Config    ProviderConfig
	Transport Transport
	Mapping   *Spec
}

// RawPayloadWriter is satisfied by audit.RawPayloadManager.
type RawPayloadWriter interface {
	Write(eventID string, payload []byte) (string, error)
}

type RequestAttributor interface {
	MatchProxyRequest(ts time.Time, provider, model string) (openclawstate.MatchResult, bool)
}
