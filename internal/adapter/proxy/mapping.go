package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Spec struct {
	Version  string       `yaml:"version"`
	Provider string       `yaml:"provider"`
	Request  RequestSpec  `yaml:"request"`
	Response ResponseSpec `yaml:"response"`
	Stream   StreamSpec   `yaml:"stream"`
}

type RequestSpec struct {
	Model        PathRule  `yaml:"model"`
	Stream       PathRule  `yaml:"stream"`
	Tools        ToolsRule `yaml:"tools"`
	InputSummary PathRule  `yaml:"input_summary"`
}

type ResponseSpec struct {
	Model        PathRule  `yaml:"model"`
	FinishReason PathRule  `yaml:"finish_reason"`
	Usage        UsageSpec `yaml:"usage"`
	Error        ErrorSpec `yaml:"error"`
}

type StreamSpec struct {
	Usage        UsageSpec `yaml:"usage"`
	FinishReason PathRule  `yaml:"finish_reason"`
}

type UsageSpec struct {
	InputTokens  PathRule `yaml:"input_tokens"`
	OutputTokens PathRule `yaml:"output_tokens"`
}

type ErrorSpec struct {
	ErrorType    PathRule `yaml:"error_type"`
	ErrorMessage PathRule `yaml:"error_message"`
}

type ToolsRule struct {
	Path string   `yaml:"path"`
	Each ToolEach `yaml:"each"`
}

type ToolEach struct {
	Name    PathRule `yaml:"name"`
	RawArgs PathRule `yaml:"raw_args"`
}

type PathRule struct {
	Path      string            `yaml:"path"`
	Default   interface{}       `yaml:"default,omitempty"`
	Truncate  int               `yaml:"truncate,omitempty"`
	Serialize string            `yaml:"serialize,omitempty"`
	Map       map[string]string `yaml:"map,omitempty"`
}

func LoadProviderSpec(mappingsDir, provider string, fallback []byte) (*Spec, error) {
	filePath := filepath.Join(mappingsDir, provider+".yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read mapping %s: %w", filePath, err)
		}
		if len(fallback) == 0 {
			return nil, fmt.Errorf("mapping file not found for provider %q", provider)
		}
		data = fallback
	}

	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse mapping: %w", err)
	}
	if strings.TrimSpace(spec.Provider) == "" {
		spec.Provider = provider
	}
	return &spec, nil
}

func NormalizeRequest(spec *Spec, provider, endpoint string, body []byte) (NormalizedRequest, error) {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return NormalizedRequest{}, fmt.Errorf("parse request body: %w", err)
	}

	out := NormalizedRequest{
		Provider: provider,
		Endpoint: endpoint,
	}

	if v, ok := extractWithRule(payload, spec.Request.Model); ok {
		out.Model = asString(v)
	}
	if v, ok := extractWithRule(payload, spec.Request.Stream); ok {
		out.Stream = asBool(v)
	}
	if v, ok := extractWithRule(payload, spec.Request.InputSummary); ok {
		out.InputSummary = asString(v)
	}

	if arr, ok := extractPath(payload, spec.Request.Tools.Path); ok {
		if tools, ok := arr.([]interface{}); ok {
			out.Tools = make([]NormalizedTool, 0, len(tools))
			for _, t := range tools {
				tool := NormalizedTool{}
				if v, ok := extractWithRule(t, spec.Request.Tools.Each.Name); ok {
					tool.Name = asString(v)
				}
				if v, ok := extractWithRule(t, spec.Request.Tools.Each.RawArgs); ok {
					tool.RawArgs = mustBytes(v)
				}
				if tool.Name != "" {
					out.Tools = append(out.Tools, tool)
				}
			}
		}
	}

	return out, nil
}

func NormalizeResponse(spec *Spec, body []byte, upstreamStatus int) (NormalizedResponse, error) {
	out := NormalizedResponse{UpstreamStatus: upstreamStatus}
	if len(body) == 0 {
		return out, nil
	}

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return out, fmt.Errorf("parse response body: %w", err)
	}

	if v, ok := extractWithRule(payload, spec.Response.Model); ok {
		out.Model = asString(v)
	}
	if v, ok := extractWithRule(payload, spec.Response.FinishReason); ok {
		out.FinishReason = asString(v)
	}
	if v, ok := extractWithRule(payload, spec.Response.Usage.InputTokens); ok {
		out.InputTokens = asInt64(v)
	}
	if v, ok := extractWithRule(payload, spec.Response.Usage.OutputTokens); ok {
		out.OutputTokens = asInt64(v)
	}
	if v, ok := extractWithRule(payload, spec.Response.Error.ErrorType); ok {
		out.ErrorType = asString(v)
	}
	if v, ok := extractWithRule(payload, spec.Response.Error.ErrorMessage); ok {
		out.ErrorMessage = asString(v)
	}

	return out, nil
}

func ApplyStreamSpec(spec *Spec, event map[string]interface{}, out *NormalizedResponse) {
	if spec == nil || out == nil || event == nil {
		return
	}
	if v, ok := extractWithRule(event, spec.Stream.Usage.InputTokens); ok {
		out.InputTokens = asInt64(v)
	}
	if v, ok := extractWithRule(event, spec.Stream.Usage.OutputTokens); ok {
		out.OutputTokens = asInt64(v)
	}
	if v, ok := extractWithRule(event, spec.Stream.FinishReason); ok {
		fr := asString(v)
		if fr != "" {
			out.FinishReason = fr
		}
	}
}

func extractWithRule(payload interface{}, rule PathRule) (interface{}, bool) {
	if rule.Path == "" {
		if rule.Default != nil {
			return rule.Default, true
		}
		return nil, false
	}

	value, ok := extractPath(payload, rule.Path)
	if !ok {
		if rule.Default != nil {
			return rule.Default, true
		}
		return nil, false
	}

	if len(rule.Map) > 0 {
		if mapped, ok := rule.Map[asString(value)]; ok {
			value = mapped
		}
	}

	if rule.Serialize == "json" {
		value = mustBytes(value)
	}

	if rule.Truncate > 0 {
		s := asString(value)
		if len(s) > rule.Truncate {
			value = s[:rule.Truncate]
		}
	}

	return value, true
}

func extractPath(payload interface{}, rawPath string) (interface{}, bool) {
	p := normalizeSelector(rawPath)
	if p == "" {
		return payload, true
	}

	current := payload
	for _, seg := range strings.Split(p, ".") {
		if seg == "" {
			continue
		}

		// support gjson-like array index token (e.g. choices.0.value)
		if idx, err := strconv.Atoi(seg); err == nil {
			arr, ok := current.([]interface{})
			if !ok {
				return nil, false
			}
			if idx < 0 {
				idx = len(arr) + idx
			}
			if idx < 0 || idx >= len(arr) {
				return nil, false
			}
			current = arr[idx]
			continue
		}

		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := obj[seg]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func normalizeSelector(selector string) string {
	s := strings.TrimSpace(selector)
	s = strings.TrimPrefix(s, "$.")
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, "[", ".")
	s = strings.ReplaceAll(s, "]", "")
	s = strings.ReplaceAll(s, "..", ".")
	s = strings.TrimPrefix(s, ".")
	return s
}

func asString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case json.RawMessage:
		return string(x)
	case []byte:
		return string(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func asBool(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	default:
		return false
	}
}

func asInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	default:
		return 0
	}
}

func mustBytes(v interface{}) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case string:
		return []byte(x)
	default:
		b, _ := json.Marshal(v)
		return b
	}
}
