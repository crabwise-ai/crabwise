package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/spf13/cobra"
)

func newAuditCmd() *cobra.Command {
	var (
		configPath string
		since      string
		until      string
		agent      string
		action     string
		session    string
		outcome    string
		triggered  bool
		limit      int
		export     string
		verify     bool
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query audit trail",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			client, err := ipc.Dial(cfg.Daemon.SocketPath)
			if err != nil {
				return fmt.Errorf("connect to daemon: %w", err)
			}
			defer client.Close()

			if verify {
				return verifyIntegrity(client)
			}

			params := map[string]interface{}{}
			if since != "" {
				params["since"] = since
			}
			if until != "" {
				params["until"] = until
			}
			if agent != "" {
				params["agent"] = agent
			}
			if action != "" {
				params["action"] = action
			}
			if session != "" {
				params["session"] = session
			}
			if outcome != "" {
				params["outcome"] = outcome
			}
			if triggered {
				params["triggered"] = true
			}
			if limit > 0 {
				params["limit"] = limit
			}

			if export == "json" {
				return exportJSON(client, params)
			}

			result, err := client.Call("audit.query", params)
			if err != nil {
				return fmt.Errorf("audit.query: %w", err)
			}

			var qr audit.QueryResult
			if err := json.Unmarshal(result, &qr); err != nil {
				return fmt.Errorf("parse result: %w", err)
			}

			if len(qr.Events) == 0 {
				fmt.Println("No events found.")
				return nil
			}

			for _, e := range qr.Events {
				ts := e.Timestamp.Format("15:04:05")
				fmt.Printf("%s [%s] %-18s %-10s %-8s %s\n", ts, e.AgentID, e.ActionType, e.Action, e.Outcome, e.SessionID)
			}
			fmt.Printf("\nTotal: %d events\n", qr.Total)

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().StringVar(&since, "since", "", "start time (RFC3339)")
	cmd.Flags().StringVar(&until, "until", "", "end time (RFC3339)")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent ID")
	cmd.Flags().StringVar(&action, "action", "", "filter by action type")
	cmd.Flags().StringVar(&session, "session", "", "filter by session ID")
	cmd.Flags().StringVar(&outcome, "outcome", "", "filter by outcome (success, warned, failure, blocked)")
	cmd.Flags().BoolVar(&triggered, "triggered", false, "show only events with triggered commandments")
	cmd.Flags().IntVar(&limit, "limit", 50, "max events to return")
	cmd.Flags().StringVar(&export, "export", "", "export format (json)")
	cmd.Flags().BoolVar(&verify, "verify-integrity", false, "verify hash chain integrity")

	return cmd
}

func verifyIntegrity(client *ipc.Client) error {
	result, err := client.Call("audit.verify", nil)
	if err != nil {
		return fmt.Errorf("audit.verify: %w", err)
	}

	var v struct {
		Valid    bool   `json:"valid"`
		Total    int    `json:"total"`
		BrokenAt string `json:"broken_at"`
	}
	if err := json.Unmarshal(result, &v); err != nil {
		return fmt.Errorf("parse verify result: %w", err)
	}

	if v.Valid {
		fmt.Printf("Hash chain valid (%d events)\n", v.Total)
	} else {
		fmt.Printf("Hash chain BROKEN at event %s (%d events checked)\n", v.BrokenAt, v.Total)
	}

	return nil
}

func exportJSON(client *ipc.Client, params map[string]interface{}) error {
	result, err := client.Call("audit.query", params)
	if err != nil {
		return fmt.Errorf("audit.query: %w", err)
	}

	var qr struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(result, &qr); err != nil {
		// Try direct array
		var events []json.RawMessage
		if err2 := json.Unmarshal(result, &events); err2 == nil {
			qr.Events = events
		}
	}

	output := struct {
		ExportedAt string            `json:"exported_at"`
		Count      int               `json:"count"`
		Events     []json.RawMessage `json:"events"`
	}{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Count:      len(qr.Events),
		Events:     qr.Events,
	}

	data, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(data))
	return nil
}
