package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const defaultStreamMaxBytes = 10 * 1024 * 1024 // 10 MB

type streamTelemetry struct {
	Model        string
	FinishReason string
	InputTokens  int64
	OutputTokens int64
	HasUsage     bool
	FirstTokenAt time.Time
}

type streamLine struct {
	data []byte
	err  error
}

type bufferedSSEResult struct {
	Body       []byte
	Telemetry  streamTelemetry
	ToolBlocks []ToolUseBlock
}

// bufferSSEStream reads the entire SSE stream into memory, collecting telemetry
// and reconstructing tool_use blocks from ToolCallDeltas. It does NOT write to w.
// Returns an error if body exceeds maxBytes or an idle timeout occurs.
// Fails closed on parse errors (returns error rather than silently skipping deltas).
func bufferSSEStream(src io.ReadCloser, transport Transport, idleTimeout time.Duration, maxBytes int64) (bufferedSSEResult, error) {
	defer src.Close()

	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Second
	}

	var result bufferedSSEResult
	var bodyBuf []byte
	var pendingEventType string

	type tcAcc struct {
		id   string
		name string
		args strings.Builder
	}
	accumulators := map[int]*tcAcc{}

	lines := make(chan streamLine, 64)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(src)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				cpy := make([]byte, len(line))
				copy(cpy, line)
				lines <- streamLine{data: cpy}
			}
			if err != nil {
				if err == io.EOF {
					return
				}
				lines <- streamLine{err: err}
				return
			}
		}
	}()

	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return result, fmt.Errorf("stream idle timeout")
		case msg, ok := <-lines:
			if !ok {
				// Channel closed = stream ended; finalize tool blocks
				result.ToolBlocks = make([]ToolUseBlock, 0, len(accumulators))
				for _, acc := range accumulators {
					rawInput := json.RawMessage(acc.args.String())
					if !json.Valid(rawInput) {
						escaped, _ := json.Marshal(acc.args.String())
						rawInput = json.RawMessage(`{"_raw_args":` + string(escaped) + `}`)
					}
					result.ToolBlocks = append(result.ToolBlocks, ToolUseBlock{
						ID:        acc.id,
						ToolName:  acc.name,
						ToolInput: rawInput,
						Targets:   ParseTargets(acc.name, rawInput),
					})
				}
				result.Body = bodyBuf
				return result, nil
			}
			if msg.err != nil {
				return result, msg.err
			}

			// Enforce buffer limit
			if int64(len(bodyBuf)+len(msg.data)) > maxBytes {
				return result, fmt.Errorf("stream buffer exceeded %d bytes", maxBytes)
			}
			bodyBuf = append(bodyBuf, msg.data...)

			// Reset idle timer
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)

			if eventType := parseSSEEventType(msg.data); eventType != "" {
				pendingEventType = eventType
				continue
			}

			payload := parseSSEData(msg.data)
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}

			if result.Telemetry.FirstTokenAt.IsZero() {
				result.Telemetry.FirstTokenAt = time.Now()
			}

			event, err := transport.ParseStreamEvent(payload)
			if err != nil {
				// Fail closed: parse error may mean a tool_call delta was dropped.
				return result, fmt.Errorf("enforcement_error: failed to parse stream event: %w", err)
			}
			event.EventType = pendingEventType
			pendingEventType = ""

			if event.Model != "" {
				result.Telemetry.Model = event.Model
			}
			if event.HasFinish {
				result.Telemetry.FinishReason = event.FinishReason
			}
			if event.HasUsage {
				result.Telemetry.InputTokens = event.InputTokens
				result.Telemetry.OutputTokens = event.OutputTokens
				result.Telemetry.HasUsage = true
			}

			// Accumulate tool call deltas
			for _, d := range event.ToolCallDeltas {
				acc, exists := accumulators[d.Index]
				if !exists {
					acc = &tcAcc{}
					accumulators[d.Index] = acc
				}
				if d.ID != "" {
					acc.id = d.ID
				}
				if d.Name != "" {
					acc.name = d.Name
				}
				acc.args.WriteString(d.ArgsDelta)
			}
		}
	}
}


func parseSSEData(line []byte) []byte {
	s := strings.TrimSpace(string(line))
	if s == "" || strings.HasPrefix(s, ":") {
		return nil
	}
	if !strings.HasPrefix(s, "data:") {
		return nil
	}
	return []byte(strings.TrimSpace(strings.TrimPrefix(s, "data:")))
}

func parseSSEEventType(line []byte) string {
	s := strings.TrimSpace(string(line))
	if strings.HasPrefix(s, "event:") {
		return strings.TrimSpace(strings.TrimPrefix(s, "event:"))
	}
	return ""
}
