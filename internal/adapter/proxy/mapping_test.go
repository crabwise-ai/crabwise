package proxy

import (
	"testing"
)

func TestToGjsonPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"$.choices[0].finish_reason", "choices.0.finish_reason"},
		{"model", "model"},
		{"$.error.type", "error.type"},
		{"choices[-1].message", "choices.@reverse.0.message"},
		{"$", ""},
		{"", ""},
		{"usage.prompt_tokens", "usage.prompt_tokens"},
	}

	for _, tt := range tests {
		got := toGjsonPath(tt.input)
		if got != tt.want {
			t.Errorf("toGjsonPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractString(t *testing.T) {
	data := []byte(`{"model":"gpt-4","choices":[{"finish_reason":"stop"}]}`)

	got := extractString(data, PathRule{Path: "model"})
	if got != "gpt-4" {
		t.Errorf("extractString model = %q, want gpt-4", got)
	}

	got = extractString(data, PathRule{Path: "$.choices.0.finish_reason"})
	if got != "stop" {
		t.Errorf("extractString finish_reason = %q, want stop", got)
	}

	got = extractString(data, PathRule{Path: "missing", Default: "fallback"})
	if got != "fallback" {
		t.Errorf("extractString missing with default = %q, want fallback", got)
	}
}

func TestExtractString_Map(t *testing.T) {
	data := []byte(`{"type":"invalid_request_error"}`)
	rule := PathRule{
		Path: "type",
		Map:  map[string]string{"invalid_request_error": "invalid_request"},
	}
	got := extractString(data, rule)
	if got != "invalid_request" {
		t.Errorf("extractString with map = %q, want invalid_request", got)
	}
}

func TestExtractString_Truncate(t *testing.T) {
	data := []byte(`{"msg":"this is a long message that should be truncated"}`)
	rule := PathRule{Path: "msg", Truncate: 10}
	got := extractString(data, rule)
	if got != "this is a " {
		t.Errorf("extractString truncated = %q, want 'this is a '", got)
	}
}

func TestNormalizeRequest_Basic(t *testing.T) {
	spec := &Spec{
		Request: RequestSpec{
			Model:  PathRule{Path: "model"},
			Stream: PathRule{Path: "stream"},
		},
	}
	body := []byte(`{"model":"gpt-4o","stream":true}`)

	req, err := NormalizeRequest(spec, "openai", "/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", req.Model)
	}
	if !req.Stream {
		t.Error("stream should be true")
	}
}

func TestNormalizeRequest_Tools(t *testing.T) {
	spec := &Spec{
		Request: RequestSpec{
			Model: PathRule{Path: "model"},
			Tools: ToolsRule{
				Path: "tools",
				Each: ToolEach{
					Name:    PathRule{Path: "function.name"},
					RawArgs: PathRule{Path: "function.arguments", Serialize: "json"},
				},
			},
		},
	}
	body := []byte(`{"model":"gpt-4o","tools":[{"function":{"name":"read_file","arguments":{"path":"/tmp/x"}}}]}`)

	req, err := NormalizeRequest(spec, "openai", "/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if req.Tools[0].Name != "read_file" {
		t.Errorf("tool name = %q, want read_file", req.Tools[0].Name)
	}
}
