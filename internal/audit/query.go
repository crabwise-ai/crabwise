package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type QueryFilter struct {
	Since         *time.Time
	Until         *time.Time
	Agent         string
	Action        string
	Session       string
	Outcome       string
	TriggeredOnly bool
	Limit         int
	Offset        int
}

type QueryResult struct {
	Events []*AuditEvent
	Total  int
}

type TokenSummaryRow struct {
	Day          string `json:"day"`
	AgentID      string `json:"agent_id"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

func QueryEvents(db *sql.DB, f QueryFilter) (*QueryResult, error) {
	var conditions []string
	var args []interface{}

	if f.Since != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.Until != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, f.Until.UTC().Format(time.RFC3339Nano))
	}
	if f.Agent != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, f.Agent)
	}
	if f.Action != "" {
		conditions = append(conditions, "action_type = ?")
		args = append(args, f.Action)
	}
	if f.Session != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, f.Session)
	}
	if f.Outcome != "" {
		conditions = append(conditions, "outcome = ?")
		args = append(args, f.Outcome)
	}
	if f.TriggeredOnly {
		conditions = append(conditions, "commandments_triggered IS NOT NULL", "commandments_triggered != ''", "commandments_triggered != '[]'")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM events " + where
	var total int
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	// Fetch events
	query := "SELECT id, timestamp, agent_id, agent_pid, action_type, action, arguments, " +
		"session_id, parent_session_id, working_dir, parser_version, outcome, " +
		"commandments_evaluated, commandments_triggered, " +
		"provider, model, tool_category, tool_effect, tool_name, taxonomy_version, classification_source, " +
		"input_tokens, output_tokens, cost_usd, " +
		"adapter_id, adapter_type, raw_payload_ref, prev_hash, event_hash, redacted, " +
		"hostname, user_id " +
		"FROM events " + where + " ORDER BY timestamp ASC"

	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	if f.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		e := &AuditEvent{}
		var ts string
		var agentPID sql.NullInt64
		var action, arguments, sessionID, parentSessionID, workingDir, parserVersion sql.NullString
		var cmdEval, cmdTrig sql.NullString
		var provider, model sql.NullString
		var toolCategory, toolEffect, toolName, taxonomyVersion, classificationSource sql.NullString
		var inputTokens, outputTokens sql.NullInt64
		var costUSD sql.NullFloat64
		var adapterID, adapterType, rawPayloadRef, prevHash sql.NullString
		var hostname, userID sql.NullString
		var redacted int

		err := rows.Scan(
			&e.ID, &ts, &e.AgentID, &agentPID,
			&e.ActionType, &action, &arguments,
			&sessionID, &parentSessionID, &workingDir, &parserVersion,
			&e.Outcome,
			&cmdEval, &cmdTrig,
			&provider, &model, &toolCategory, &toolEffect, &toolName, &taxonomyVersion, &classificationSource,
			&inputTokens, &outputTokens, &costUSD,
			&adapterID, &adapterType, &rawPayloadRef, &prevHash, &e.EventHash, &redacted,
			&hostname, &userID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		e.AgentPID = int(agentPID.Int64)
		e.Action = action.String
		e.Arguments = arguments.String
		e.SessionID = sessionID.String
		e.ParentSessionID = parentSessionID.String
		e.WorkingDir = workingDir.String
		e.ParserVersion = parserVersion.String
		e.CommandmentsEvaluated = cmdEval.String
		e.CommandmentsTriggered = cmdTrig.String
		e.Provider = provider.String
		e.Model = model.String
		e.ToolCategory = toolCategory.String
		e.ToolEffect = toolEffect.String
		e.ToolName = toolName.String
		e.TaxonomyVersion = taxonomyVersion.String
		e.ClassificationSource = classificationSource.String
		e.InputTokens = inputTokens.Int64
		e.OutputTokens = outputTokens.Int64
		e.CostUSD = costUSD.Float64
		e.AdapterID = adapterID.String
		e.AdapterType = adapterType.String
		e.RawPayloadRef = rawPayloadRef.String
		e.PrevHash = prevHash.String
		e.Hostname = hostname.String
		e.UserID = userID.String
		e.Redacted = redacted != 0

		events = append(events, e)
	}

	return &QueryResult{Events: events, Total: total}, nil
}

func ExportJSON(events []*AuditEvent) ([]byte, error) {
	return json.MarshalIndent(events, "", "  ")
}

func QueryTokenSummary(db *sql.DB, f QueryFilter) ([]TokenSummaryRow, error) {
	var conditions []string
	var args []interface{}

	if f.Since != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.Until != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, f.Until.UTC().Format(time.RFC3339Nano))
	}
	if f.Agent != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, f.Agent)
	}

	conditions = append(conditions, "action_type = 'ai_request'")
	where := "WHERE " + strings.Join(conditions, " AND ")

	query := `SELECT substr(timestamp, 1, 10) AS day, agent_id, COALESCE(model, '') AS model,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens
		FROM events ` + where + `
		GROUP BY day, agent_id, model
		ORDER BY day ASC, agent_id ASC, model ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query token summary: %w", err)
	}
	defer rows.Close()

	out := []TokenSummaryRow{}
	for rows.Next() {
		var r TokenSummaryRow
		if err := rows.Scan(&r.Day, &r.AgentID, &r.Model, &r.InputTokens, &r.OutputTokens); err != nil {
			return nil, fmt.Errorf("scan token summary: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// VerifyIntegrity walks the event chain and returns the first broken link.
func VerifyIntegrity(db *sql.DB) (valid bool, total int, brokenAt string, err error) {
	rows, err := db.Query("SELECT id, prev_hash, event_hash FROM events ORDER BY timestamp ASC, rowid ASC")
	if err != nil {
		return false, 0, "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	prevHash := "genesis"
	for rows.Next() {
		var id, storedPrev, storedHash string
		if err := rows.Scan(&id, &storedPrev, &storedHash); err != nil {
			return false, total, "", fmt.Errorf("scan: %w", err)
		}
		total++

		if storedPrev != prevHash {
			return false, total, id, nil
		}
		prevHash = storedHash
	}

	return true, total, "", nil
}
