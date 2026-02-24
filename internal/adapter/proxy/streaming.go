package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type streamTelemetry struct {
	Model        string
	FinishReason string
	InputTokens  int64
	OutputTokens int64
	HasUsage     bool
}

type streamLine struct {
	data []byte
	err  error
}

func proxySSEStream(w http.ResponseWriter, src io.ReadCloser, transport Transport, idleTimeout time.Duration) (streamTelemetry, error) {
	defer src.Close()

	out := streamTelemetry{}
	flusher, _ := w.(http.Flusher)

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
			return out, fmt.Errorf("stream idle timeout")
		case msg, ok := <-lines:
			if !ok {
				return out, nil
			}
			if msg.err != nil {
				return out, msg.err
			}
			if _, err := w.Write(msg.data); err != nil {
				return out, err
			}
			if flusher != nil {
				flusher.Flush()
			}

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)

			payload := parseSSEData(msg.data)
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}
			event, err := transport.ParseStreamEvent(payload)
			if err != nil {
				continue // passthrough priority
			}
			if event.Model != "" {
				out.Model = event.Model
			}
			if event.HasFinish {
				out.FinishReason = event.FinishReason
			}
			if event.HasUsage {
				out.InputTokens = event.InputTokens
				out.OutputTokens = event.OutputTokens
				out.HasUsage = true
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
