package proxy

import (
	"testing"
)

func TestParseSSEData(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		want     string
		wantNil  bool
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
