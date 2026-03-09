package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// WebhookBackend sends HTTP POST notifications for blocked events.
type WebhookBackend struct {
	url           string
	authHeaderEnv string
	format        string
	minInterval   time.Duration
	client        *http.Client
	mu            sync.Mutex
	lastSent      time.Time
}

// NewWebhookBackend creates a webhook notification backend.
func NewWebhookBackend(cfg WebhookConfig) *WebhookBackend {
	return &WebhookBackend{
		url:           cfg.URL,
		authHeaderEnv: cfg.AuthHeaderEnv,
		format:        cfg.Format,
		minInterval:   cfg.MinInterval,
		client:        &http.Client{Timeout: 5 * time.Second},
	}
}

func (w *WebhookBackend) Name() string { return "webhook" }

type webhookPayload struct {
	Event     string `json:"event"`
	Agent     string `json:"agent"`
	Action    string `json:"action"`
	Rule      string `json:"rule,omitempty"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp"`
}

func (w *WebhookBackend) Send(ctx context.Context, evt *audit.AuditEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.minInterval > 0 && time.Since(w.lastSent) < w.minInterval {
		return nil
	}

	// Extract first triggered rule name + message if available
	var ruleName, ruleMsg string
	if evt.CommandmentsTriggered != "" {
		var triggered []audit.TriggeredRule
		if err := json.Unmarshal([]byte(evt.CommandmentsTriggered), &triggered); err == nil && len(triggered) > 0 {
			ruleName = triggered[0].Name
			ruleMsg = triggered[0].Message
		}
	}

	var (
		body []byte
		err  error
	)
	if w.format == "discord" {
		content := fmt.Sprintf("🚫 Agent **%s** blocked: %s", evt.AgentID, evt.Action)
		if ruleName != "" {
			content += fmt.Sprintf(" (%s)", ruleName)
		}
		if ruleMsg != "" {
			content += fmt.Sprintf(" — %s", ruleMsg)
		}
		body, err = json.Marshal(map[string]string{"content": content})
	} else {
		body, err = json.Marshal(webhookPayload{
			Event:     "block",
			Agent:     evt.AgentID,
			Action:    evt.Action,
			Rule:      ruleName,
			Message:   ruleMsg,
			Timestamp: evt.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if w.authHeaderEnv != "" {
		if authVal := os.Getenv(w.authHeaderEnv); authVal != "" {
			req.Header.Set("Authorization", authVal)
		}
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	w.lastSent = time.Now()
	return nil
}
