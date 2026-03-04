package proxy

import "testing"

func TestOpenAIParseStreamEvent_FirstToolCallChunk(t *testing.T) {
	data := []byte(`{
		"choices": [{
			"delta": {
				"tool_calls": [{
					"index": 0,
					"id": "call_abc",
					"type": "function",
					"function": {"name": "Bash", "arguments": ""}
				}]
			}
		}]
	}`)
	tr := NewOpenAITransport(ProviderConfig{}, 0)
	event, err := tr.ParseStreamEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.ToolCallDeltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(event.ToolCallDeltas))
	}
	d := event.ToolCallDeltas[0]
	if d.Index != 0 {
		t.Errorf("expected index 0, got %d", d.Index)
	}
	if d.ID != "call_abc" {
		t.Errorf("expected id call_abc, got %q", d.ID)
	}
	if d.Name != "Bash" {
		t.Errorf("expected name Bash, got %q", d.Name)
	}
}

func TestOpenAIParseStreamEvent_ArgumentFragment(t *testing.T) {
	data := []byte(`{
		"choices": [{
			"delta": {
				"tool_calls": [{
					"index": 0,
					"function": {"arguments": "{\"comma"}
				}]
			}
		}]
	}`)
	tr := NewOpenAITransport(ProviderConfig{}, 0)
	event, err := tr.ParseStreamEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.ToolCallDeltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(event.ToolCallDeltas))
	}
	if event.ToolCallDeltas[0].ArgsDelta != `{"comma` {
		t.Errorf("expected args fragment, got %q", event.ToolCallDeltas[0].ArgsDelta)
	}
}

func TestOpenAIParseStreamEvent_NoToolCalls(t *testing.T) {
	data := []byte(`{"choices": [{"delta": {"content": "Hello"}}]}`)
	tr := NewOpenAITransport(ProviderConfig{}, 0)
	event, err := tr.ParseStreamEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.ToolCallDeltas) != 0 {
		t.Fatalf("expected 0 deltas, got %d", len(event.ToolCallDeltas))
	}
}
