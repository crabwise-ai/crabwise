package proxy

import (
	"context"
	"net/http"
	"time"
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
	MappingsDir         string
	MappingStrictMode   bool
	Providers           map[string]ProviderConfig
	Pricing             map[string]Pricing
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
}

type StreamEvent struct {
	Model        string
	FinishReason string
	InputTokens  int64
	OutputTokens int64
	HasUsage     bool
	HasFinish    bool
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
