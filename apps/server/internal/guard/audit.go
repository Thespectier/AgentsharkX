package guard

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type rawTrafficEntry struct {
	Timestamp float64 `json:"ts"`
	Action    string  `json:"action"`
	LatencyMS float64 `json:"latency_ms"`
	Risk      float64 `json:"risk"`
}

type rawAuditEntry struct {
	Event struct {
		EventID   string `json:"event_id"`
		Timestamp int64  `json:"ts_ms"`
		EventType string `json:"event_type"`
		Principal struct {
			AgentID   string `json:"agent_id"`
			SessionID string `json:"session_id"`
			UserID    string `json:"user_id"`
		} `json:"principal"`
		ToolCall struct {
			ToolName string `json:"tool_name"`
			Source   string `json:"source"`
			MCP      struct {
				UniqueID  string `json:"mcp_unique_id"`
				Name      string `json:"mcp_name"`
				ToolName  string `json:"mcp_tool_name"`
				Transport string `json:"mcp_transport"`
			} `json:"mcp"`
		} `json:"tool_call"`
	} `json:"event"`
	Decision struct {
		Action       string   `json:"action"`
		RiskScore    float64  `json:"risk_score"`
		MatchedRules []string `json:"matched_rules"`
		RuleVersion  string   `json:"rule_version"`
		PolicyID     string   `json:"policy_id"`
	} `json:"decision"`
}

// Traffic returns only the scalar fields needed for aggregate metrics. The
// upstream plugin result and reason fields are deliberately never projected.
func (client *Client) Traffic(ctx context.Context, limit int) (model.AuditFeed, error) {
	var response []rawTrafficEntry
	path := "/v1/backend/traffic"
	query := url.Values{"n": []string{strconv.Itoa(clampAuditLimit(limit))}}
	if _, err := client.upstream.GetJSONQuery(ctx, path, query, &response); err != nil {
		return model.AuditFeed{}, err
	}
	if response == nil {
		return model.AuditFeed{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	records := make([]model.AuditTrafficRecord, 0, len(response))
	for _, entry := range response {
		if entry.Timestamp <= 0 {
			continue
		}
		seconds, fraction := mathModf(entry.Timestamp)
		records = append(records, model.AuditTrafficRecord{
			Timestamp: time.Unix(seconds, int64(fraction*float64(time.Second))).UTC(),
			Action:    strings.ToUpper(entry.Action), LatencyMS: entry.LatencyMS, Risk: entry.Risk,
		})
	}
	return model.AuditFeed{Status: "available", Traffic: records}, nil
}

// Audit returns a redacted projection of AgentGuard's recent audit records.
// Runtime state, tool arguments/results, plugin results, and free-form reasons
// are intentionally not represented by the adapter types.
func (client *Client) Audit(ctx context.Context, limit int) (model.AuditFeed, error) {
	var response []rawAuditEntry
	path := "/v1/backend/audit/recent"
	query := url.Values{"n": []string{strconv.Itoa(clampAuditLimit(limit))}}
	if _, err := client.upstream.GetJSONQuery(ctx, path, query, &response); err != nil {
		return model.AuditFeed{}, err
	}
	if response == nil {
		return model.AuditFeed{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	events := make([]model.UnifiedEvent, 0, len(response))
	for index, entry := range response {
		field := fmt.Sprintf("%s/%d/event", path, index)
		if entry.Event.EventID == "" {
			return model.AuditFeed{}, &ContractError{Field: field + "/event_id", Problem: "required field is missing"}
		}
		if entry.Event.Timestamp <= 0 {
			return model.AuditFeed{}, &ContractError{Field: field + "/ts_ms", Problem: "required timestamp is missing"}
		}
		if entry.Event.EventType == "" {
			return model.AuditFeed{}, &ContractError{Field: field + "/event_type", Problem: "required field is missing"}
		}
		events = append(events, normalizeAudit(entry))
	}
	return model.AuditFeed{Status: "available", Events: events}, nil
}

func (client *Client) AuditSessions(ctx context.Context) ([]model.AuditSession, error) {
	sessions, _, err := client.sessions(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.AuditSession, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, model.AuditSession{
			ID: session.ID, UpstreamID: session.UpstreamID, AgentID: session.AgentID,
			AgentUpstreamID: session.AgentUpstreamID, Principal: session.UserID,
			LastSeen: session.LastSeen, Status: session.Status, Source: model.SourceAgentGuard,
			RawRef: session.RawRef,
		})
	}
	return items, nil
}

func normalizeAudit(entry rawAuditEntry) model.UnifiedEvent {
	timestamp := time.UnixMilli(entry.Event.Timestamp).UTC()
	action := strings.ToUpper(entry.Decision.Action)
	severity := severityForAction(action, entry.Decision.RiskScore)
	tool := entry.Event.ToolCall.ToolName
	summary := strings.ReplaceAll(entry.Event.EventType, "_", " ")
	if tool != "" {
		summary += " for " + tool
	}
	if action != "" {
		summary += " was " + strings.ToLower(action)
	}
	raw := map[string]any{
		"event": map[string]any{
			"eventId": entry.Event.EventID, "timestamp": timestamp, "eventType": entry.Event.EventType,
			"principal": map[string]any{
				"agentId": entry.Event.Principal.AgentID, "sessionId": entry.Event.Principal.SessionID,
				"userId": entry.Event.Principal.UserID,
			},
			"tool": map[string]any{
				"name": tool, "source": entry.Event.ToolCall.Source,
				"mcpUniqueId": entry.Event.ToolCall.MCP.UniqueID, "mcpName": entry.Event.ToolCall.MCP.Name,
				"mcpToolName": entry.Event.ToolCall.MCP.ToolName, "mcpTransport": entry.Event.ToolCall.MCP.Transport,
			},
		},
		"decision": map[string]any{
			"action": action, "riskScore": entry.Decision.RiskScore,
			"matchedRules": append([]string(nil), entry.Decision.MatchedRules...),
			"ruleVersion":  entry.Decision.RuleVersion, "policyId": entry.Decision.PolicyID,
		},
		"redacted": []string{"tool arguments", "tool result", "runtime state", "plugin result", "reason"},
	}
	correlation := (*model.EventCorrelation)(nil)
	if entry.Event.Principal.SessionID != "" {
		correlation = &model.EventCorrelation{SessionID: entry.Event.Principal.SessionID, Verified: false}
	}
	return model.UnifiedEvent{
		ID: "guard:" + entry.Event.EventID, Timestamp: timestamp, Source: model.SourceAgentGuard,
		Kind: "audit", Severity: severity,
		Subject: &model.EventSubject{
			AgentID: entry.Event.Principal.AgentID, PrincipalID: entry.Event.Principal.UserID,
			SessionID: entry.Event.Principal.SessionID,
		},
		Target: &model.EventTarget{Tool: tool}, Phase: phaseForEvent(entry.Event.EventType),
		Action: action, Decision: action, Correlation: correlation, Summary: summary,
		RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: entry.Event.EventID}, Raw: raw,
	}
}

func phaseForEvent(eventType string) string {
	return map[string]string{
		"llm_input": "llm_before", "llm_output": "llm_after",
		"tool_invoke": "tool_before", "tool_result": "tool_after",
	}[eventType]
}

func severityForAction(action string, risk float64) string {
	switch action {
	case "DENY":
		return "high"
	case "HUMAN_CHECK", "LLM_CHECK", "DEGRADE":
		return "medium"
	}
	if risk >= 0.8 {
		return "high"
	}
	if risk >= 0.5 {
		return "medium"
	}
	return "info"
}

func clampAuditLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
