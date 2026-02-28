package cli

import "strings"

func fullAgentLabel(agentID, sessionID string) string {
	if agentID != "openclaw" || sessionID == "" {
		return agentID
	}
	return agentID + " " + sessionID
}

func compactAgentLabel(agentID, sessionID string) string {
	if agentID != "openclaw" || sessionID == "" {
		return agentID
	}
	return "openclaw/" + compactSessionKey(sessionID)
}

func compactSessionKey(sessionID string) string {
	parts := strings.Split(sessionID, ":")
	if len(parts) >= 4 {
		return parts[1] + ":" + parts[2] + ":" + parts[len(parts)-1]
	}
	if len(sessionID) <= 24 {
		return sessionID
	}
	return sessionID[:12] + "..." + sessionID[len(sessionID)-8:]
}
