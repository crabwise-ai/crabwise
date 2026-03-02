package openclaw

import (
	"encoding/json"
	"fmt"
	"strings"
)

type GatewayFrame interface {
	isGatewayFrame()
}

type RequestFrame struct {
	Type   string      `json:"type"`
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

func (*RequestFrame) isGatewayFrame() {}

type ResponseFrame struct {
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	OK      bool           `json:"ok"`
	Payload interface{}    `json:"payload,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

func (*ResponseFrame) isGatewayFrame() {}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type EventFrame struct {
	Type         string      `json:"type"`
	Event        string      `json:"event"`
	Payload      interface{} `json:"payload,omitempty"`
	Seq          int64       `json:"seq,omitempty"`
	StateVersion *StateVer   `json:"stateVersion,omitempty"`
}

func (*EventFrame) isGatewayFrame() {}

type HelloOK struct {
	Type     string        `json:"type"`
	Protocol int           `json:"protocol"`
	Snapshot HelloSnapshot `json:"snapshot"`
	Features HelloFeatures `json:"features"`
}

func (*HelloOK) isGatewayFrame() {}

type HelloSnapshot struct {
	Presence     []PresenceEntry `json:"presence"`
	Health       interface{}     `json:"health"`
	StateVersion StateVer        `json:"stateVersion"`
}

type StateVer struct {
	Presence int64 `json:"presence"`
	Health   int64 `json:"health"`
}

type HelloFeatures struct {
	Methods []string `json:"methods"`
	Events  []string `json:"events"`
}

type PresenceEntry struct {
	Key         string     `json:"key"`
	Client      ClientInfo `json:"client"`
	ConnectedAt int64      `json:"connectedAt"`
}

type ClientInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Mode        string `json:"mode"`
}

type ChatEvent struct {
	RunID        string      `json:"runId"`
	SessionKey   string      `json:"sessionKey"`
	Seq          int64       `json:"seq"`
	State        string      `json:"state"`
	Message      interface{} `json:"message,omitempty"`
	ErrorMessage string      `json:"errorMessage,omitempty"`
	Usage        *TokenUsage `json:"usage,omitempty"`
	StopReason   string      `json:"stopReason,omitempty"`
}

type TokenUsage struct {
	InputTokens  int64 `json:"inputTokens,omitempty"`
	OutputTokens int64 `json:"outputTokens,omitempty"`
}

type AgentEvent struct {
	RunID      string                 `json:"runId"`
	Seq        int64                  `json:"seq"`
	Stream     string                 `json:"stream"`
	TS         int64                  `json:"ts"`
	Data       map[string]interface{} `json:"data"`
	SessionKey string                 `json:"sessionKey,omitempty"`
}

type ExecStartedEvent struct {
	PID       int64  `json:"pid"`
	Command   string `json:"command"`
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	StartedAt int64  `json:"startedAt"`
}

type ExecCompletedEvent struct {
	PID        int64  `json:"pid"`
	RunID      string `json:"runId"`
	SessionID  string `json:"sessionId,omitempty"`
	ExitCode   int    `json:"exitCode"`
	DurationMS int64  `json:"durationMs"`
	Status     string `json:"status"`
}

type SessionInfo struct {
	Key            string      `json:"key"`
	AgentID        string      `json:"agentId"`
	CreatedAt      int64       `json:"createdAt"`
	LastActivityAt int64       `json:"lastActivityAt"`
	MessageCount   int64       `json:"messageCount"`
	LastMessage    interface{} `json:"lastMessage,omitempty"`
	SpawnedBy      string      `json:"spawnedBy,omitempty"`
	Model          string      `json:"model,omitempty"`
	ContextTokens  int64       `json:"contextTokens,omitempty"`
	TotalTokens    int64       `json:"totalTokens,omitempty"`
	ThinkingLevel  string      `json:"thinkingLevel,omitempty"`
}

type SessionKey struct {
	AgentID   string
	Platform  string
	Recipient string
}

func ParseSessionKey(key string) SessionKey {
	parts := strings.Split(key, ":")
	out := SessionKey{
		AgentID:   "unknown",
		Platform:  "unknown",
		Recipient: "",
	}
	if len(parts) > 1 && parts[1] != "" {
		out.AgentID = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		out.Platform = parts[2]
	}
	if len(parts) > 3 {
		out.Recipient = strings.Join(parts[3:], ":")
	}
	return out
}

func DecodeGatewayFrame(data []byte) (GatewayFrame, error) {
	var probe struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	switch probe.Type {
	case "req":
		var frame RequestFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			return nil, err
		}
		return &frame, nil
	case "res":
		var frame ResponseFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			return nil, err
		}
		return &frame, nil
	case "event":
		return decodeEventFrame(probe.Event, data)
	case "hello-ok":
		var frame HelloOK
		if err := json.Unmarshal(data, &frame); err != nil {
			return nil, err
		}
		return &frame, nil
	default:
		return nil, fmt.Errorf("unknown gateway frame type %q", probe.Type)
	}
}

func decodeEventFrame(event string, data []byte) (GatewayFrame, error) {
	var frame struct {
		Type         string          `json:"type"`
		Event        string          `json:"event"`
		Payload      json.RawMessage `json:"payload"`
		Seq          int64           `json:"seq,omitempty"`
		StateVersion *StateVer       `json:"stateVersion,omitempty"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return nil, err
	}

	out := &EventFrame{
		Type:         frame.Type,
		Event:        frame.Event,
		Seq:          frame.Seq,
		StateVersion: frame.StateVersion,
	}

	switch event {
	case "chat":
		var payload ChatEvent
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return nil, err
		}
		out.Payload = &payload
	case "agent":
		var payload AgentEvent
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return nil, err
		}
		out.Payload = &payload
	case "exec.started":
		var payload ExecStartedEvent
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return nil, err
		}
		out.Payload = &payload
	case "exec.completed":
		var payload ExecCompletedEvent
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return nil, err
		}
		out.Payload = &payload
	default:
		var payload interface{}
		if len(frame.Payload) > 0 {
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				return nil, err
			}
		}
		out.Payload = payload
	}

	return out, nil
}
