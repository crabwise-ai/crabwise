package openclaw

import "testing"

func TestParseSessionKey(t *testing.T) {
	got := ParseSessionKey("agent:main:discord:channel:123")
	if got.AgentID != "main" {
		t.Fatalf("expected agent id main, got %q", got.AgentID)
	}
	if got.Platform != "discord" {
		t.Fatalf("expected platform discord, got %q", got.Platform)
	}
	if got.Recipient != "channel:123" {
		t.Fatalf("expected recipient channel:123, got %q", got.Recipient)
	}
}

func TestDecodeGatewayFrame(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		check func(t *testing.T, frame GatewayFrame)
	}{
		{
			name:  "chat event",
			frame: `{"type":"event","event":"chat","payload":{"runId":"run-1","sessionKey":"agent:main:discord:channel:123","seq":2,"state":"final","usage":{"inputTokens":10,"outputTokens":20}}}`,
			check: func(t *testing.T, frame GatewayFrame) {
				event, ok := frame.(*EventFrame)
				if !ok {
					t.Fatalf("expected EventFrame, got %T", frame)
				}
				payload, ok := event.Payload.(*ChatEvent)
				if !ok {
					t.Fatalf("expected ChatEvent, got %T", event.Payload)
				}
				if payload.RunID != "run-1" {
					t.Fatalf("expected run id run-1, got %q", payload.RunID)
				}
				if payload.SessionKey != "agent:main:discord:channel:123" {
					t.Fatalf("expected session key, got %q", payload.SessionKey)
				}
				if payload.Usage == nil || payload.Usage.InputTokens != 10 || payload.Usage.OutputTokens != 20 {
					t.Fatalf("expected token usage, got %#v", payload.Usage)
				}
			},
		},
		{
			name:  "agent event",
			frame: `{"type":"event","event":"agent","payload":{"runId":"run-2","seq":3,"stream":"stdout","ts":1730000000000,"data":{"line":"hello"},"sessionKey":"agent:main:discord:channel:123"}}`,
			check: func(t *testing.T, frame GatewayFrame) {
				event := frame.(*EventFrame)
				payload, ok := event.Payload.(*AgentEvent)
				if !ok {
					t.Fatalf("expected AgentEvent, got %T", event.Payload)
				}
				if payload.Stream != "stdout" {
					t.Fatalf("expected stdout stream, got %q", payload.Stream)
				}
				if payload.Data["line"] != "hello" {
					t.Fatalf("expected agent event data, got %#v", payload.Data)
				}
			},
		},
		{
			name:  "exec started event",
			frame: `{"type":"event","event":"exec.started","payload":{"pid":123,"command":"npm test","sessionId":"agent:main:discord:channel:123","runId":"run-3","startedAt":1730000000001}}`,
			check: func(t *testing.T, frame GatewayFrame) {
				event := frame.(*EventFrame)
				payload, ok := event.Payload.(*ExecStartedEvent)
				if !ok {
					t.Fatalf("expected ExecStartedEvent, got %T", event.Payload)
				}
				if payload.Command != "npm test" {
					t.Fatalf("expected command npm test, got %q", payload.Command)
				}
				if payload.SessionID != "agent:main:discord:channel:123" {
					t.Fatalf("expected session id, got %q", payload.SessionID)
				}
			},
		},
		{
			name:  "exec completed event",
			frame: `{"type":"event","event":"exec.completed","payload":{"pid":123,"runId":"run-3","sessionId":"agent:main:discord:channel:123","exitCode":0,"durationMs":1500,"status":"completed"}}`,
			check: func(t *testing.T, frame GatewayFrame) {
				event := frame.(*EventFrame)
				payload, ok := event.Payload.(*ExecCompletedEvent)
				if !ok {
					t.Fatalf("expected ExecCompletedEvent, got %T", event.Payload)
				}
				if payload.ExitCode != 0 {
					t.Fatalf("expected exit code 0, got %d", payload.ExitCode)
				}
				if payload.DurationMS != 1500 {
					t.Fatalf("expected duration 1500ms, got %d", payload.DurationMS)
				}
			},
		},
		{
			name:  "hello ok frame",
			frame: `{"type":"hello-ok","protocol":3,"snapshot":{"presence":[],"health":{},"stateVersion":{"presence":1,"health":2}},"features":{"methods":["sessions.list"],"events":["chat"]}}`,
			check: func(t *testing.T, frame GatewayFrame) {
				hello, ok := frame.(*HelloOK)
				if !ok {
					t.Fatalf("expected HelloOK, got %T", frame)
				}
				if hello.Protocol != 3 {
					t.Fatalf("expected protocol 3, got %d", hello.Protocol)
				}
				if len(hello.Features.Methods) != 1 || hello.Features.Methods[0] != "sessions.list" {
					t.Fatalf("unexpected methods: %#v", hello.Features.Methods)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := DecodeGatewayFrame([]byte(tc.frame))
			if err != nil {
				t.Fatalf("unmarshal frame: %v", err)
			}
			tc.check(t, frame)
		})
	}
}
