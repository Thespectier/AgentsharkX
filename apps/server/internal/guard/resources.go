package guard

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type ContractError struct {
	Field   string
	Problem string
}

func (err *ContractError) Error() string {
	return fmt.Sprintf("AgentGuard contract mismatch at %s: %s", err.Field, err.Problem)
}

type rawSessions struct {
	Sessions []rawSession `json:"sessions"`
}

type rawSession struct {
	SessionID string  `json:"session_id"`
	AgentID   string  `json:"agent_id"`
	UserID    string  `json:"user_id"`
	LastSeen  float64 `json:"last_seen"`
}

type rawLabels struct {
	Boundary    string   `json:"boundary"`
	Sensitivity string   `json:"sensitivity"`
	Integrity   string   `json:"integrity"`
	Tags        []string `json:"tags"`
}

type rawTool struct {
	OwnerAgentID string     `json:"owner_agent_id"`
	Name         string     `json:"name"`
	Labels       *rawLabels `json:"labels"`
}

type rawDetection struct {
	ObjectID         string   `json:"object_id"`
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	RiskLevel        string   `json:"risk_level"`
	Capabilities     []string `json:"capabilities"`
	RiskLabels       []string `json:"risk_labels"`
	PolicyTargets    []string `json:"policy_targets"`
	SuggestedPlugins []string `json:"suggested_plugins"`
}

type rawSkill struct {
	OwnerAgentID  string        `json:"owner_agent_id"`
	AgentID       string        `json:"agent_id"`
	SessionID     string        `json:"session_id"`
	SkillUniqueID string        `json:"skill_unique_id"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Framework     string        `json:"source_framework"`
	Detection     *rawDetection `json:"detect_result"`
}

type rawMCP struct {
	OwnerAgentID string        `json:"owner_agent_id"`
	AgentID      string        `json:"agent_id"`
	SessionID    string        `json:"session_id"`
	MCPUniqueID  string        `json:"mcp_unique_id"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Framework    string        `json:"source_framework"`
	Transport    string        `json:"transport"`
	Remote       *bool         `json:"remote"`
	ToolCount    *int          `json:"tool_count"`
	Detection    *rawDetection `json:"detect_result"`
}

type labelResponse struct {
	OK   bool    `json:"ok"`
	Tool rawTool `json:"tool"`
}

type rawDetectionResponse struct {
	OK                    bool                 `json:"ok"`
	AgentID               string               `json:"agent_id"`
	Requested             int                  `json:"requested"`
	Detected              int                  `json:"detected"`
	MissingSkillUniqueIDs []string             `json:"missing_skill_unique_ids"`
	MissingMCPUniqueIDs   []string             `json:"missing_mcp_unique_ids"`
	Results               []rawDetectionResult `json:"results"`
}

type rawDetectionResult struct {
	SkillUniqueID string       `json:"skill_unique_id"`
	MCPUniqueID   string       `json:"mcp_unique_id"`
	Name          string       `json:"name"`
	Detection     rawDetection `json:"detect_result"`
}

func (client *Client) TrustSnapshot(ctx context.Context) (model.TrustSnapshot, error) {
	type sessionsResult struct {
		items     []model.TrustSession
		fetchedAt time.Time
		err       error
	}
	type resourcesResult struct {
		items     []model.TrustResource
		fetchedAt time.Time
		err       error
	}

	var sessions sessionsResult
	var tools, skills, mcps resourcesResult
	var wait sync.WaitGroup
	wait.Add(4)
	go func() {
		defer wait.Done()
		sessions.items, sessions.fetchedAt, sessions.err = client.sessions(ctx)
	}()
	go func() {
		defer wait.Done()
		tools.items, tools.fetchedAt, tools.err = client.tools(ctx)
	}()
	go func() {
		defer wait.Done()
		skills.items, skills.fetchedAt, skills.err = client.skills(ctx)
	}()
	go func() {
		defer wait.Done()
		mcps.items, mcps.fetchedAt, mcps.err = client.mcps(ctx)
	}()
	wait.Wait()

	results := []struct {
		name      string
		fetchedAt time.Time
		err       error
	}{
		{"sessions", sessions.fetchedAt, sessions.err},
		{"tools", tools.fetchedAt, tools.err},
		{"skills", skills.fetchedAt, skills.err},
		{"mcps", mcps.fetchedAt, mcps.err},
	}
	snapshot := model.TrustSnapshot{
		Sessions:  sessions.items,
		Resources: append(append(append([]model.TrustResource{}, tools.items...), skills.items...), mcps.items...),
	}
	var failures []error
	for _, result := range results {
		if result.err != nil {
			failures = append(failures, result.err)
			snapshot.Failures = append(snapshot.Failures, trustFailure(result.name, result.err))
			continue
		}
		if result.fetchedAt.After(snapshot.FetchedAt) {
			snapshot.FetchedAt = result.fetchedAt
		}
	}
	if len(failures) == len(results) {
		return model.TrustSnapshot{}, errors.Join(failures...)
	}
	return snapshot, nil
}

func (client *Client) sessions(ctx context.Context) ([]model.TrustSession, time.Time, error) {
	const path = "/v1/backend/sessions"
	var response rawSessions
	if _, err := client.upstream.GetJSON(ctx, path, &response); err != nil {
		return nil, time.Time{}, err
	}
	if response.Sessions == nil {
		return nil, time.Time{}, &ContractError{Field: path + "/sessions", Problem: "required array is missing"}
	}
	fetchedAt := time.Now().UTC()
	items := make([]model.TrustSession, 0, len(response.Sessions))
	for index, item := range response.Sessions {
		field := fmt.Sprintf("%s/sessions/%d", path, index)
		if item.SessionID == "" {
			return nil, time.Time{}, &ContractError{Field: field + "/session_id", Problem: "required field is missing"}
		}
		var lastSeen *time.Time
		if item.LastSeen > 0 {
			seconds, fraction := mathModf(item.LastSeen)
			value := time.Unix(seconds, int64(fraction*float64(time.Second))).UTC()
			lastSeen = &value
		}
		items = append(items, model.TrustSession{
			TrustResourceBase: trustBase("session", item.SessionID+"\x00"+item.AgentID+"\x00"+item.UserID, item.SessionID, field, fetchedAt),
			AgentID:           opaqueAgentID(item.AgentID), AgentUpstreamID: item.AgentID, UserID: item.UserID,
			LastSeen: lastSeen, Status: "unknown",
		})
	}
	return items, fetchedAt, nil
}

func (client *Client) tools(ctx context.Context) ([]model.TrustResource, time.Time, error) {
	const path = "/v1/backend/tools"
	var response []rawTool
	if _, err := client.upstream.GetJSON(ctx, path, &response); err != nil {
		return nil, time.Time{}, err
	}
	if response == nil {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	fetchedAt := time.Now().UTC()
	items := make([]model.TrustResource, 0, len(response))
	for index, item := range response {
		field := fmt.Sprintf("%s/%d", path, index)
		resource, err := normalizeTool(item, field, fetchedAt)
		if err != nil {
			return nil, time.Time{}, err
		}
		items = append(items, resource)
	}
	return items, fetchedAt, nil
}

func (client *Client) skills(ctx context.Context) ([]model.TrustResource, time.Time, error) {
	const path = "/v1/backend/skills"
	var response []rawSkill
	if _, err := client.upstream.GetJSON(ctx, path, &response); err != nil {
		return nil, time.Time{}, err
	}
	if response == nil {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	fetchedAt := time.Now().UTC()
	items := make([]model.TrustResource, 0, len(response))
	for index, item := range response {
		field := fmt.Sprintf("%s/%d", path, index)
		owner, err := verifiedOwner(item.OwnerAgentID, item.AgentID, field)
		if err != nil {
			return nil, time.Time{}, err
		}
		if item.SkillUniqueID == "" {
			return nil, time.Time{}, &ContractError{Field: field + "/skill_unique_id", Problem: "required field is missing"}
		}
		if item.Name == "" {
			return nil, time.Time{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
		}
		items = append(items, model.TrustResource{
			TrustResourceBase: trustBase("skill", owner+"\x00"+item.SkillUniqueID, item.SkillUniqueID, field, fetchedAt),
			Name:              item.Name, Type: "skill", OwnerAgentID: stableID("agent", owner), OwnerAgentUpstreamID: owner,
			SessionID: item.SessionID, Description: boundedText(item.Description), Framework: item.Framework,
			Detection: normalizeDetection(item.Detection, item.SkillUniqueID, item.Name),
		})
	}
	return items, fetchedAt, nil
}

func (client *Client) mcps(ctx context.Context) ([]model.TrustResource, time.Time, error) {
	const path = "/v1/backend/mcps"
	var response []rawMCP
	if _, err := client.upstream.GetJSON(ctx, path, &response); err != nil {
		return nil, time.Time{}, err
	}
	if response == nil {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	fetchedAt := time.Now().UTC()
	items := make([]model.TrustResource, 0, len(response))
	for index, item := range response {
		field := fmt.Sprintf("%s/%d", path, index)
		owner, err := verifiedOwner(item.OwnerAgentID, item.AgentID, field)
		if err != nil {
			return nil, time.Time{}, err
		}
		if item.MCPUniqueID == "" {
			return nil, time.Time{}, &ContractError{Field: field + "/mcp_unique_id", Problem: "required field is missing"}
		}
		if item.Name == "" {
			return nil, time.Time{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
		}
		items = append(items, model.TrustResource{
			TrustResourceBase: trustBase("mcp", owner+"\x00"+item.MCPUniqueID, item.MCPUniqueID, field, fetchedAt),
			Name:              item.Name, Type: "mcp", OwnerAgentID: stableID("agent", owner), OwnerAgentUpstreamID: owner,
			SessionID: item.SessionID, Description: boundedText(item.Description), Framework: item.Framework,
			Transport: item.Transport, Remote: item.Remote, ToolCount: item.ToolCount,
			Detection: normalizeDetection(item.Detection, item.MCPUniqueID, item.Name),
		})
	}
	return items, fetchedAt, nil
}

func (client *Client) UpdateToolLabels(ctx context.Context, agentID, toolName string, update model.TrustLabelUpdate) (model.TrustResource, error) {
	path := "/v1/backend/agents/" + url.PathEscape(agentID) + "/tools/" + url.PathEscape(toolName) + "/labels"
	body := struct {
		Boundary    *string   `json:"boundary,omitempty"`
		Sensitivity *string   `json:"sensitivity,omitempty"`
		Integrity   *string   `json:"integrity,omitempty"`
		Tags        *[]string `json:"tags,omitempty"`
	}{update.Boundary, update.Sensitivity, update.Integrity, update.Tags}
	var response labelResponse
	if _, err := client.upstream.PatchJSON(ctx, path, body, &response); err != nil {
		return model.TrustResource{}, err
	}
	if !response.OK {
		return model.TrustResource{}, &ContractError{Field: path + "/ok", Problem: "expected true"}
	}
	return normalizeTool(response.Tool, path+"/tool", time.Now().UTC())
}

func (client *Client) DetectSkills(ctx context.Context, agentID string, resourceIDs []string, useLLM bool) ([]model.TrustDetection, []string, error) {
	body := struct {
		SkillUniqueIDs []string `json:"skill_unique_ids"`
		UseLLM         bool     `json:"use_llm"`
	}{resourceIDs, useLLM}
	return client.detect(ctx, "/v1/backend/agents/"+url.PathEscape(agentID)+"/skills/detect", agentID, "skill", body)
}

func (client *Client) DetectMCPs(ctx context.Context, agentID string, resourceIDs []string) ([]model.TrustDetection, []string, error) {
	body := struct {
		MCPUniqueIDs []string `json:"mcp_unique_ids"`
	}{resourceIDs}
	return client.detect(ctx, "/v1/backend/agents/"+url.PathEscape(agentID)+"/mcps/detect", agentID, "mcp", body)
}

func (client *Client) detect(ctx context.Context, path, agentID, resourceType string, body any) ([]model.TrustDetection, []string, error) {
	var response rawDetectionResponse
	if _, err := client.operations.PostMutationJSON(ctx, path, body, &response); err != nil {
		return nil, nil, err
	}
	if !response.OK {
		return nil, nil, &ContractError{Field: path + "/ok", Problem: "expected true"}
	}
	if response.AgentID != agentID {
		return nil, nil, &ContractError{Field: path + "/agent_id", Problem: "did not match the requested agent"}
	}
	if response.Results == nil {
		return nil, nil, &ContractError{Field: path + "/results", Problem: "required array is missing"}
	}
	results := make([]model.TrustDetection, 0, len(response.Results))
	for index, item := range response.Results {
		upstreamID := item.SkillUniqueID
		if resourceType == "mcp" {
			upstreamID = item.MCPUniqueID
		}
		if upstreamID == "" {
			return nil, nil, &ContractError{Field: fmt.Sprintf("%s/results/%d/%s_unique_id", path, index, resourceType), Problem: "required field is missing"}
		}
		detection := normalizeDetection(&item.Detection, upstreamID, item.Name)
		if detection == nil {
			return nil, nil, &ContractError{Field: fmt.Sprintf("%s/results/%d/detect_result", path, index), Problem: "required result is missing"}
		}
		results = append(results, *detection)
	}
	warnings := []string{}
	missing := response.MissingSkillUniqueIDs
	if resourceType == "mcp" {
		missing = response.MissingMCPUniqueIDs
	}
	if len(missing) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d requested %s resource(s) were no longer available", len(missing), resourceType))
	}
	return results, warnings, nil
}

func normalizeTool(item rawTool, field string, fetchedAt time.Time) (model.TrustResource, error) {
	if item.OwnerAgentID == "" {
		return model.TrustResource{}, &ContractError{Field: field + "/owner_agent_id", Problem: "required field is missing"}
	}
	if item.Name == "" {
		return model.TrustResource{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
	}
	if item.Labels == nil {
		return model.TrustResource{}, &ContractError{Field: field + "/labels", Problem: "required object is missing"}
	}
	labels := &model.TrustLabels{
		Boundary: item.Labels.Boundary, Sensitivity: item.Labels.Sensitivity,
		Integrity: item.Labels.Integrity, Tags: append([]string{}, item.Labels.Tags...),
	}
	return model.TrustResource{
		TrustResourceBase: trustBase("tool", item.OwnerAgentID+"\x00"+item.Name, item.Name, field, fetchedAt),
		Name:              item.Name, Type: "tool", OwnerAgentID: stableID("agent", item.OwnerAgentID),
		OwnerAgentUpstreamID: item.OwnerAgentID, Labels: labels,
	}, nil
}

func normalizeDetection(item *rawDetection, upstreamID, name string) *model.TrustDetection {
	if item == nil {
		return nil
	}
	return &model.TrustDetection{
		ResourceUpstreamID: upstreamID, Name: valueOr(item.Name, name), Label: item.Label,
		RiskLevel: valueOr(item.RiskLevel, "unknown"), Capabilities: append([]string{}, item.Capabilities...),
		RiskLabels: append([]string{}, item.RiskLabels...), PolicyTargets: append([]string{}, item.PolicyTargets...),
		SuggestedPlugins: append([]string{}, item.SuggestedPlugins...),
	}
}

func verifiedOwner(owner, agent, field string) (string, error) {
	if owner == "" {
		return "", &ContractError{Field: field + "/owner_agent_id", Problem: "required field is missing"}
	}
	if agent != "" && agent != owner {
		return "", &ContractError{Field: field + "/agent_id", Problem: "did not match owner_agent_id"}
	}
	return owner, nil
}

func trustBase(kind, identity, upstreamID, rawRef string, fetchedAt time.Time) model.TrustResourceBase {
	return model.TrustResourceBase{
		ID: stableID(kind, identity), UpstreamID: upstreamID, Source: model.SourceAgentGuard,
		FetchedAt: fetchedAt, RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: rawRef},
	}
}

func stableID(kind, identity string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(kind + "\x00" + identity))
}

func opaqueAgentID(upstreamID string) string {
	if upstreamID == "" {
		return ""
	}
	return stableID("agent", upstreamID)
}

func trustFailure(capability string, err error) model.SourceFailure {
	code := "UPSTREAM_UNAVAILABLE"
	message := "AgentGuard " + capability + " could not be read"
	var contractError *ContractError
	if errors.As(err, &contractError) {
		code = "UPSTREAM_CONTRACT_MISMATCH"
		message = contractError.Error()
	}
	return model.SourceFailure{Source: model.SourceAgentGuard, Code: code, Message: message}
}

func boundedText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}

func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func mathModf(value float64) (int64, float64) {
	seconds := int64(value)
	return seconds, value - float64(seconds)
}
