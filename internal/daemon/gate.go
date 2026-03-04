package daemon

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/ipc"
)

// GateEvaluateParams is the JSON-RPC request schema for gate.evaluate.
type GateEvaluateParams struct {
	AgentID      string      `json:"agent_id"`
	ToolName     string      `json:"tool_name"`
	ToolCategory string      `json:"tool_category"`
	ToolEffect   string      `json:"tool_effect"`
	Targets      GateTargets `json:"targets,omitempty"`
}

// GateTargets mirrors the structured targets schema from the proxy ToolTargets.
type GateTargets struct {
	Argv     []string `json:"argv,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	PathMode string   `json:"path_mode,omitempty"`
}

// GateEvaluateResult is the JSON-RPC response for gate.evaluate.
type GateEvaluateResult struct {
	GateEventID   string `json:"gate_event_id"`
	Decision      string `json:"decision"` // "block" | "pass"
	CommandmentID string `json:"commandment_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Enforcement   string `json:"enforcement,omitempty"`
}

// makeGateEvaluateHandler returns an ipc.Handler for the gate.evaluate method.
func makeGateEvaluateHandler(svc CommandmentsService, emit func(*audit.AuditEvent)) ipc.Handler {
	return func(raw json.RawMessage) (interface{}, error) {
		var params GateEvaluateParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, err
		}

		eventID := uuid.NewString()

		// Map targets into Arguments so argument-sensitive commandment rules can fire.
		targetsJSON, _ := json.Marshal(map[string]interface{}{
			"argv":      params.Targets.Argv,
			"paths":     params.Targets.Paths,
			"path_mode": params.Targets.PathMode,
		})

		e := &audit.AuditEvent{
			ID:           eventID,
			Timestamp:    time.Now().UTC(),
			AgentID:      params.AgentID,
			ActionType:   audit.ActionToolCall,
			Action:       params.ToolName,
			ToolName:     params.ToolName,
			ToolCategory: params.ToolCategory,
			ToolEffect:   params.ToolEffect,
			Arguments:    string(targetsJSON),
			AdapterID:    "gate",
			AdapterType:  "gate",
		}

		result := svc.Evaluate(e)
		for _, triggered := range result.Triggered {
			if strings.EqualFold(triggered.Enforcement, "block") {
				e.Outcome = audit.OutcomeBlocked
				emit(e)
				return GateEvaluateResult{
					GateEventID:   eventID,
					Decision:      "block",
					CommandmentID: triggered.Name,
					Reason:        triggered.Message,
					Enforcement:   "block",
				}, nil
			}
		}

		// Pass-through: no audit emission (by design — too high volume).
		return GateEvaluateResult{
			GateEventID: eventID,
			Decision:    "pass",
		}, nil
	}
}

// RegisterGateEvaluate wires the gate.evaluate handler onto the IPC server.
func RegisterGateEvaluate(srv *ipc.Server, svc CommandmentsService, emit func(*audit.AuditEvent)) {
	srv.Handle("gate.evaluate", makeGateEvaluateHandler(svc, emit))
}
