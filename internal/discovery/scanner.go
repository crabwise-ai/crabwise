package discovery

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var codexSessionIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type AgentInfo struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	PID            int       `json:"pid"`
	Status         string    `json:"status"` // active, inactive
	SessionFile    string    `json:"session_file,omitempty"`
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	DiscoveredAt   time.Time `json:"discovered_at"`
}

// ScanProcesses scans /proc for processes matching signatures.
func ScanProcesses(signatures []string) []AgentInfo {
	var agents []AgentInfo

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return agents
	}

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}

		cmdStr := strings.ReplaceAll(string(cmdline), "\x00", " ")
		cmdStr = strings.TrimSpace(cmdStr)

		for _, sig := range signatures {
			if strings.Contains(strings.ToLower(cmdStr), strings.ToLower(sig)) {
				agents = append(agents, AgentInfo{
					ID:           agentIDFromPID(sig, pid),
					Type:         sig,
					PID:          pid,
					Status:       "active",
					DiscoveredAt: time.Now().UTC(),
				})
				break
			}
		}
	}

	return agents
}

// ScanLogPaths scans log directories for session files.
func ScanLogPaths(logPaths []string) []AgentInfo {
	var agents []AgentInfo
	seen := make(map[string]bool)

	for _, logPath := range logPaths {
		if err := filepath.Walk(logPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") {
				return nil
			}

			agentType := detectAgentType(path)
			sessionID := extractSessionIDForAgent(path, agentType)
			agentSessionID := agentType + "/" + sessionID
			if sessionID == "" || seen[agentSessionID] {
				return nil
			}
			seen[agentSessionID] = true
			agents = append(agents, AgentInfo{
				ID:           agentType + "/" + sessionID,
				Type:         agentType,
				Status:       sessionStatus(info),
				SessionFile:  path,
				DiscoveredAt: time.Now().UTC(),
			})
			return nil
		}); err != nil {
			log.Printf("discovery: walk %s: %v", logPath, err)
		}
	}

	return agents
}

func agentIDFromPID(agentType string, pid int) string {
	return agentType + "/pid-" + strconv.Itoa(pid)
}

func extractSessionIDForAgent(path, agentType string) string {
	base := filepath.Base(path)
	sessionID := strings.TrimSuffix(base, ".jsonl")

	if agentType == "codex-cli" {
		if match := codexSessionIDPattern.FindString(sessionID); match != "" {
			return strings.ToLower(match)
		}
	}

	return sessionID
}

func detectAgentType(path string) string {
	if strings.Contains(path, ".claude") {
		return "claude-code"
	}
	if strings.Contains(path, ".codex") {
		return "codex-cli"
	}
	return "unknown"
}

func sessionStatus(info os.FileInfo) string {
	if time.Since(info.ModTime()) < 5*time.Minute {
		return "active"
	}
	return "inactive"
}
