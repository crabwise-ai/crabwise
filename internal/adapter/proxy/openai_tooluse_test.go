package proxy

import (
	"testing"
)

func TestOpenAIExtractToolUseBlocks_SingleToolCall(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{
					"id": "call_abc",
					"type": "function",
					"function": {
						"name": "Bash",
						"arguments": "{\"command\":\"rm -rf /tmp/test\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	tr := NewOpenAITransport(ProviderConfig{}, 0)
	blocks, err := tr.ExtractToolUseBlocks(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].ID != "call_abc" {
		t.Errorf("expected id call_abc, got %q", blocks[0].ID)
	}
	if blocks[0].ToolName != "Bash" {
		t.Errorf("expected tool Bash, got %q", blocks[0].ToolName)
	}
	if len(blocks[0].Targets.Argv) == 0 {
		t.Error("expected argv parsed from command")
	}
}

func TestOpenAIExtractToolUseBlocks_NoToolCalls(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}]
	}`)

	tr := NewOpenAITransport(ProviderConfig{}, 0)
	blocks, err := tr.ExtractToolUseBlocks(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestOpenAIExtractToolUseBlocks_MalformedArguments(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {
				"tool_calls": [{
					"id": "call_x",
					"function": {"name": "Bash", "arguments": "not-json"}
				}]
			}
		}]
	}`)

	tr := NewOpenAITransport(ProviderConfig{}, 0)
	blocks, err := tr.ExtractToolUseBlocks(body)
	// Should not error — malformed args result in a block with raw input preserved.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block even with malformed args, got %d", len(blocks))
	}
}
