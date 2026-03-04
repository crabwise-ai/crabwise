package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func init() {
	RegisterTransport("openai", func(cfg ProviderConfig, timeout time.Duration) Transport {
		return NewOpenAITransport(cfg, timeout)
	})
}

type OpenAITransport struct {
	client   *http.Client
	authMode string
	authKey  string
}

func NewOpenAITransport(cfg ProviderConfig, upstreamTimeout time.Duration) *OpenAITransport {
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 10
	}
	idleTimeout := cfg.IdleConnTimeout
	if idleTimeout <= 0 {
		idleTimeout = 90 * time.Second
	}

	tr := &http.Transport{
		MaxIdleConns:    maxIdle,
		IdleConnTimeout: idleTimeout,
	}

	return &OpenAITransport{
		client: &http.Client{
			Transport: tr,
			Timeout:   upstreamTimeout,
		},
		authMode: strings.ToLower(strings.TrimSpace(cfg.AuthMode)),
		authKey:  strings.TrimSpace(cfg.AuthKey),
	}
}

func (t *OpenAITransport) PrepareAuth(req *http.Request) error {
	if t.authMode != "configured" {
		return nil
	}
	if t.authKey == "" {
		return fmt.Errorf("configured auth_mode requires auth_key")
	}

	key := t.authKey
	if strings.HasPrefix(key, "env:") {
		key = os.Getenv(strings.TrimPrefix(key, "env:"))
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("configured auth_key resolved empty value")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	return nil
}

func (t *OpenAITransport) Forward(ctx context.Context, req *http.Request) (*http.Response, error) {
	return t.client.Do(req.WithContext(ctx))
}

func (t *OpenAITransport) ParseStreamEvent(data []byte) (StreamEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return StreamEvent{}, err
	}

	out := StreamEvent{}

	if model, ok := payload["model"].(string); ok {
		out.Model = model
	}

	if usage, ok := payload["usage"].(map[string]interface{}); ok {
		if v, ok := usage["prompt_tokens"]; ok {
			out.InputTokens = toInt64(v)
			out.HasUsage = true
		}
		if v, ok := usage["completion_tokens"]; ok {
			out.OutputTokens = toInt64(v)
			out.HasUsage = true
		}
	}

	if choices, ok := payload["choices"].([]interface{}); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]interface{}); ok {
			if fr, ok := c0["finish_reason"].(string); ok && fr != "" {
				out.FinishReason = fr
				out.HasFinish = true
			}
		}
	}

	return out, nil
}

func (t *OpenAITransport) ExtractToolUseBlocks(body []byte) ([]ToolUseBlock, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var blocks []ToolUseBlock
	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			rawInput := json.RawMessage(tc.Function.Arguments)
			// Preserve malformed args as {"_raw_args": "<original>"} for forensic context.
			if !json.Valid(rawInput) {
				escaped, _ := json.Marshal(string(rawInput))
				rawInput = json.RawMessage(`{"_raw_args":` + string(escaped) + `}`)
			}
			blocks = append(blocks, ToolUseBlock{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				ToolInput: rawInput,
				Targets:   ParseTargets(tc.Function.Name, rawInput),
			})
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return blocks, nil
}

func toInt64(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		return 0
	}
}
