package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
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
	if !gjson.ValidBytes(body) {
		return NormalizedRequest{}, fmt.Errorf("parse request body: invalid JSON")
	}

	out := NormalizedRequest{
		Provider: provider,
		Endpoint: endpoint,
	}

	out.Model = extractString(body, spec.Request.Model)
	out.Stream = extractBool(body, spec.Request.Stream)
	out.InputSummary = extractString(body, spec.Request.InputSummary)

	toolsPath := toGjsonPath(spec.Request.Tools.Path)
	if toolsPath != "" {
		toolsResult := gjson.GetBytes(body, toolsPath)
		if toolsResult.IsArray() {
			toolsResult.ForEach(func(_, value gjson.Result) bool {
				raw := []byte(value.Raw)
				tool := NormalizedTool{}
				tool.Name = extractString(raw, spec.Request.Tools.Each.Name)
				if spec.Request.Tools.Each.RawArgs.Serialize == "json" {
					r := gjsonGet(raw, spec.Request.Tools.Each.RawArgs)
					if r.Exists() {
						tool.RawArgs = []byte(r.Raw)
					}
				} else {
					tool.RawArgs = []byte(extractString(raw, spec.Request.Tools.Each.RawArgs))
				}
				if tool.Name != "" {
					out.Tools = append(out.Tools, tool)
				}
				return true
			})
		}
	}

	return out, nil
}

func NormalizeResponse(spec *Spec, body []byte, upstreamStatus int) (NormalizedResponse, error) {
	out := NormalizedResponse{UpstreamStatus: upstreamStatus}
	if len(body) == 0 {
		return out, nil
	}
	if !gjson.ValidBytes(body) {
		return out, fmt.Errorf("parse response body: invalid JSON")
	}

	out.Model = extractString(body, spec.Response.Model)
	out.FinishReason = extractString(body, spec.Response.FinishReason)
	out.InputTokens = extractInt64(body, spec.Response.Usage.InputTokens)
	out.OutputTokens = extractInt64(body, spec.Response.Usage.OutputTokens)
	out.ErrorType = extractString(body, spec.Response.Error.ErrorType)
	out.ErrorMessage = extractString(body, spec.Response.Error.ErrorMessage)

	return out, nil
}

func ApplyStreamSpec(spec *Spec, rawEvent []byte, out *NormalizedResponse) {
	if spec == nil || out == nil || len(rawEvent) == 0 {
		return
	}
	if !gjson.ValidBytes(rawEvent) {
		return
	}

	if v := extractInt64(rawEvent, spec.Stream.Usage.InputTokens); v != 0 {
		out.InputTokens = v
	}
	if v := extractInt64(rawEvent, spec.Stream.Usage.OutputTokens); v != 0 {
		out.OutputTokens = v
	}
	if v := extractString(rawEvent, spec.Stream.FinishReason); v != "" {
		out.FinishReason = v
	}
}

func extractString(data []byte, rule PathRule) string {
	r := gjsonGet(data, rule)
	if !r.Exists() {
		if rule.Default != nil {
			return fmt.Sprint(rule.Default)
		}
		return ""
	}

	value := r.String()

	if len(rule.Map) > 0 {
		if mapped, ok := rule.Map[value]; ok {
			value = mapped
		}
	}
	if rule.Truncate > 0 && len(value) > rule.Truncate {
		value = value[:rule.Truncate]
	}
	return value
}

func extractBool(data []byte, rule PathRule) bool {
	r := gjsonGet(data, rule)
	if !r.Exists() {
		if rule.Default != nil {
			switch v := rule.Default.(type) {
			case bool:
				return v
			case string:
				return strings.EqualFold(v, "true")
			}
		}
		return false
	}
	return r.Bool()
}

func extractInt64(data []byte, rule PathRule) int64 {
	r := gjsonGet(data, rule)
	if !r.Exists() {
		return 0
	}
	return r.Int()
}

func gjsonGet(data []byte, rule PathRule) gjson.Result {
	p := toGjsonPath(rule.Path)
	if p == "" {
		return gjson.Result{}
	}
	return gjson.GetBytes(data, p)
}

// toGjsonPath converts $.foo[0].bar notation to gjson dot notation: foo.0.bar.
// Negative indexes like [-1] are translated to gjson's @reverse.0 idiom.
func toGjsonPath(selector string) string {
	s := strings.TrimSpace(selector)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "$.")
	s = strings.TrimPrefix(s, "$")
	if s == "" {
		return ""
	}

	s = negIndexRE.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[1 : len(match)-1] // strip [ ]
		if inner == "-1" {
			return ".@reverse.0"
		}
		return "." + inner
	})

	s = strings.ReplaceAll(s, "[", ".")
	s = strings.ReplaceAll(s, "]", "")
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", ".")
	}
	s = strings.TrimPrefix(s, ".")
	return s
}

var negIndexRE = regexp.MustCompile(`\[-?\d+\]`)

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
