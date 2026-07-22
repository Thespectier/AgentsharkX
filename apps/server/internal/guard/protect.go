package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const (
	maxProtectItems = 500
	maxPluginAgents = 20
)

type rawRuntimeRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RuleID      string `json:"rule_id"`
	Status      string `json:"status"`
	ToolPattern string `json:"tool_pattern"`
	Action      string `json:"action"`
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Reason      string `json:"reason"`
	PackID      string `json:"pack_id"`
	UserManaged *bool  `json:"user_managed"`
}

type rawCheckMessage struct {
	Message string `json:"message"`
}

type rawRuleCheck struct {
	OK        bool              `json:"ok"`
	RuleCount int               `json:"rule_count"`
	Errors    []rawCheckMessage `json:"errors"`
	Warnings  []rawCheckMessage `json:"warnings"`
	Hints     []rawCheckMessage `json:"hints"`
}

type rawRuleMutation struct {
	OK      bool   `json:"ok"`
	AgentID string `json:"agent_id"`
	PackID  string `json:"pack_id"`
	RuleID  string `json:"rule_id"`
	Created *bool  `json:"created"`
}

type rawApproval struct {
	TicketID string `json:"ticket_id"`
	Created  *int64 `json:"created_ms"`
	Status   string `json:"status"`
	Event    struct {
		EventID   string `json:"event_id"`
		Timestamp *int64 `json:"ts_ms"`
		EventType string `json:"event_type"`
		Principal struct {
			AgentID   string `json:"agent_id"`
			SessionID string `json:"session_id"`
			UserID    string `json:"user_id"`
		} `json:"principal"`
		ToolCall struct {
			ToolName string `json:"tool_name"`
		} `json:"tool_call"`
	} `json:"event"`
	Decision struct {
		Action       string   `json:"action"`
		RiskScore    float64  `json:"risk_score"`
		MatchedRules []string `json:"matched_rules"`
		Reason       string   `json:"reason"`
	} `json:"decision"`
}

type rawApprovalMutation struct {
	OK bool `json:"ok"`
}

type rawPluginConfig struct {
	AgentID      string          `json:"agent_id"`
	PluginConfig json.RawMessage `json:"plugin_config"`
	ConfigSource string          `json:"config_source"`
}

type rawPluginAvailable struct {
	AgentID       string            `json:"agent_id"`
	LocalPlugins  []rawPluginOption `json:"local_plugins"`
	RemotePlugins []rawPluginOption `json:"remote_plugins"`
}

type rawPluginOption struct {
	Name   string   `json:"name"`
	Phases []string `json:"phases"`
}

type pluginConfigView struct {
	Source string
	Phases map[string]pluginPhaseView
}

type pluginPhaseView struct {
	Local  []string
	Remote []string
}

func (client *Client) RuntimeRules(ctx context.Context) ([]model.RuntimeRule, time.Time, error) {
	const path = "/v1/backend/rules"
	var response []rawRuntimeRule
	if _, err := client.upstream.GetJSON(ctx, path, &response); err != nil {
		return nil, time.Time{}, err
	}
	if response == nil {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	if len(response) > maxProtectItems {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "response exceeds the supported item limit"}
	}
	fetchedAt := time.Now().UTC()
	items := make([]model.RuntimeRule, 0, len(response))
	for index, raw := range response {
		field := fmt.Sprintf("%s/%d", path, index)
		item, err := normalizeRuntimeRule(raw, field, fetchedAt)
		if err != nil {
			return nil, time.Time{}, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AgentUpstreamID == items[j].AgentUpstreamID {
			return items[i].UpstreamID < items[j].UpstreamID
		}
		return items[i].AgentUpstreamID < items[j].AgentUpstreamID
	})
	return items, fetchedAt, nil
}

func (client *Client) CheckRuntimeRule(ctx context.Context, source string) (model.RuntimeRuleCheck, error) {
	const path = "/v1/backend/rules/check"
	var response rawRuleCheck
	if _, err := client.upstream.PostJSON(ctx, path, struct {
		Source string `json:"source"`
	}{Source: source}, &response); err != nil {
		return model.RuntimeRuleCheck{}, err
	}
	if response.Errors == nil || response.Warnings == nil || response.Hints == nil {
		return model.RuntimeRuleCheck{}, &ContractError{Field: path, Problem: "errors, warnings, and hints arrays are required"}
	}
	errorsList, err := normalizeCheckMessages(response.Errors, path+"/errors")
	if err != nil {
		return model.RuntimeRuleCheck{}, err
	}
	warnings, err := normalizeCheckMessages(response.Warnings, path+"/warnings")
	if err != nil {
		return model.RuntimeRuleCheck{}, err
	}
	hints, err := normalizeCheckMessages(response.Hints, path+"/hints")
	if err != nil {
		return model.RuntimeRuleCheck{}, err
	}
	if response.OK != (len(errorsList) == 0) {
		return model.RuntimeRuleCheck{}, &ContractError{Field: path + "/ok", Problem: "did not match the errors array"}
	}
	return model.RuntimeRuleCheck{
		Source: model.SourceAgentGuard, OK: response.OK, RuleCount: response.RuleCount,
		Errors: errorsList, Warnings: warnings, Hints: hints,
	}, nil
}

func (client *Client) PublishRuntimeRule(ctx context.Context, agentID, source string) (string, error) {
	path := "/v1/backend/agents/" + url.PathEscape(agentID) + "/rules"
	var response rawRuleMutation
	if _, err := client.operations.PostMutationJSON(ctx, path, struct {
		Source string `json:"source"`
	}{Source: source}, &response); err != nil {
		return "", err
	}
	if !response.OK || response.AgentID != agentID || response.PackID != "agent::"+agentID || response.RuleID == "" || response.Created == nil || !*response.Created {
		return "", &ContractError{Field: path, Problem: "publish response did not confirm the requested agent and created rule"}
	}
	return response.RuleID, nil
}

func (client *Client) DeleteRuntimeRule(ctx context.Context, agentID, ruleID string) error {
	path := "/v1/backend/agents/" + url.PathEscape(agentID) + "/rules/" + url.PathEscape(ruleID)
	var response rawRuleMutation
	if _, err := client.operations.DeleteMutationJSON(ctx, path, nil, &response); err != nil {
		return err
	}
	if !response.OK || response.AgentID != agentID || response.PackID != "agent::"+agentID || response.RuleID != ruleID {
		return &ContractError{Field: path, Problem: "delete response did not confirm the requested rule"}
	}
	return nil
}

func (client *Client) Approvals(ctx context.Context) ([]model.Approval, time.Time, error) {
	const path = "/v1/backend/approvals"
	var response []rawApproval
	if _, err := client.upstream.GetJSON(ctx, path, &response); err != nil {
		return nil, time.Time{}, err
	}
	if response == nil {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "expected an array"}
	}
	if len(response) > maxProtectItems {
		return nil, time.Time{}, &ContractError{Field: path, Problem: "response exceeds the supported item limit"}
	}
	fetchedAt := time.Now().UTC()
	items := make([]model.Approval, 0, len(response))
	for index, raw := range response {
		field := fmt.Sprintf("%s/%d", path, index)
		item, err := normalizeApproval(raw, field, fetchedAt)
		if err != nil {
			return nil, time.Time{}, err
		}
		items = append(items, item)
	}
	return items, fetchedAt, nil
}

func (client *Client) ResolveApproval(ctx context.Context, ticketID, decision, note string) error {
	if decision != "approve" && decision != "deny" {
		return errors.New("invalid approval decision")
	}
	path := "/v1/backend/approvals/" + url.PathEscape(ticketID) + "/" + decision
	var response rawApprovalMutation
	if _, err := client.operations.PostMutationJSON(ctx, path, struct {
		Note string `json:"note"`
	}{Note: note}, &response); err != nil {
		return err
	}
	if !response.OK {
		return &ContractError{Field: path + "/ok", Problem: "expected true"}
	}
	return nil
}

func (client *Client) ProtectPlugins(ctx context.Context) (model.ProtectPluginSnapshot, error) {
	trust, err := client.TrustSnapshot(ctx)
	if err != nil {
		return model.ProtectPluginSnapshot{}, err
	}
	agentIDs := explicitAgentIDs(trust)
	snapshot := model.ProtectPluginSnapshot{Failures: append([]model.SourceFailure{}, trust.Failures...)}
	if len(agentIDs) > maxPluginAgents {
		agentIDs = agentIDs[:maxPluginAgents]
		snapshot.Failures = append(snapshot.Failures, model.SourceFailure{
			Source: model.SourceAgentGuard, Code: "PLUGIN_AGENT_LIMIT",
			Message: fmt.Sprintf("AgentGuard plugin discovery is limited to %d explicit agents", maxPluginAgents),
		})
	}
	type result struct {
		items     []model.ProtectPluginPhase
		fetchedAt time.Time
		failures  []model.SourceFailure
	}
	results := make([]result, len(agentIDs))
	var wait sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	for index, identity := range agentIDs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index] = client.pluginPhases(ctx, identity.opaque, identity.upstream, semaphore)
		}()
	}
	wait.Wait()
	for _, result := range results {
		snapshot.Items = append(snapshot.Items, result.items...)
		snapshot.Failures = append(snapshot.Failures, result.failures...)
		if result.fetchedAt.After(snapshot.FetchedAt) {
			snapshot.FetchedAt = result.fetchedAt
		}
	}
	if snapshot.FetchedAt.IsZero() {
		snapshot.FetchedAt = trust.FetchedAt
	}
	return snapshot, nil
}

func (client *Client) pluginPhases(ctx context.Context, opaqueAgentID, agentID string, semaphore chan struct{}) (out struct {
	items     []model.ProtectPluginPhase
	fetchedAt time.Time
	failures  []model.SourceFailure
}) {
	configPath := "/v1/backend/agents/" + url.PathEscape(agentID) + "/plugins/config"
	availablePath := "/v1/backend/agents/" + url.PathEscape(agentID) + "/plugins/available"
	var config rawPluginConfig
	var available rawPluginAvailable
	var configErr, availableErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		semaphore <- struct{}{}
		_, configErr = client.upstream.GetJSON(ctx, configPath, &config)
		<-semaphore
	}()
	go func() {
		defer wait.Done()
		semaphore <- struct{}{}
		_, availableErr = client.upstream.GetJSON(ctx, availablePath, &available)
		<-semaphore
	}()
	wait.Wait()
	out.fetchedAt = time.Now().UTC()
	if configErr != nil {
		out.failures = append(out.failures, trustFailure("plugin config", configErr))
	}
	if availableErr != nil {
		out.failures = append(out.failures, trustFailure("available plugins", availableErr))
	}
	if configErr != nil && availableErr != nil {
		return out
	}
	configView := pluginConfigView{Source: "unavailable", Phases: map[string]pluginPhaseView{}}
	if configErr == nil {
		view, err := normalizePluginConfig(config, configPath, agentID)
		if err != nil {
			out.failures = append(out.failures, trustFailure("plugin config", err))
		} else {
			configView = view
		}
	}
	availableLocal := map[string][]string{}
	availableRemote := map[string][]string{}
	if availableErr == nil {
		local, remote, err := normalizeAvailablePlugins(available, availablePath, agentID)
		if err != nil {
			out.failures = append(out.failures, trustFailure("available plugins", err))
		} else {
			availableLocal, availableRemote = local, remote
		}
	}
	phases := []string{"llm_before", "llm_after", "tool_before", "tool_after"}
	phaseSet := map[string]struct{}{}
	for _, phase := range phases {
		phaseSet[phase] = struct{}{}
	}
	for phase := range configView.Phases {
		if _, ok := phaseSet[phase]; !ok {
			phases = append(phases, phase)
			phaseSet[phase] = struct{}{}
		}
	}
	for phase := range availableLocal {
		if _, ok := phaseSet[phase]; !ok {
			phases = append(phases, phase)
			phaseSet[phase] = struct{}{}
		}
	}
	for phase := range availableRemote {
		if _, ok := phaseSet[phase]; !ok {
			phases = append(phases, phase)
			phaseSet[phase] = struct{}{}
		}
	}
	for _, phase := range phases {
		configured := configView.Phases[phase]
		rawRef := configPath + "#phase=" + phase
		out.items = append(out.items, model.ProtectPluginPhase{
			ProtectResourceBase: model.ProtectResourceBase{
				ID: stableID("plugin-phase", agentID+"\x00"+phase), UpstreamID: agentID + ":" + phase,
				Source: model.SourceAgentGuard, FetchedAt: out.fetchedAt,
				RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: rawRef},
			},
			AgentID: opaqueAgentID, AgentUpstreamID: agentID, Phase: phase, ConfigSource: configView.Source,
			EnabledLocalPlugins: append([]string{}, configured.Local...), EnabledRemotePlugins: append([]string{}, configured.Remote...),
			AvailableLocalPlugins: append([]string{}, availableLocal[phase]...), AvailableRemotePlugins: append([]string{}, availableRemote[phase]...),
		})
	}
	return out
}

func normalizeRuntimeRule(raw rawRuntimeRule, field string, fetchedAt time.Time) (model.RuntimeRule, error) {
	if raw.RuleID == "" || raw.Name == "" || raw.Status == "" || raw.Action == "" || raw.PackID == "" || raw.UserManaged == nil {
		return model.RuntimeRule{}, &ContractError{Field: field, Problem: "rule_id, name, status, action, pack_id, and user_managed are required"}
	}
	if raw.ID != "" && raw.ID != raw.RuleID {
		return model.RuntimeRule{}, &ContractError{Field: field + "/id", Problem: "did not match rule_id"}
	}
	switch raw.Action {
	case "ALLOW", "DENY", "HUMAN_CHECK", "LLM_CHECK", "DEGRADE":
	default:
		return model.RuntimeRule{}, &ContractError{Field: field + "/action", Problem: "unsupported action"}
	}
	agentUpstreamID := strings.TrimPrefix(raw.PackID, "agent::")
	if agentUpstreamID == raw.PackID {
		agentUpstreamID = ""
	}
	agentID := opaqueAgentID(agentUpstreamID)
	scope := "global"
	if agentUpstreamID != "" {
		scope = "agent:" + agentUpstreamID
	}
	return model.RuntimeRule{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: stableID("runtime-rule", raw.PackID+"\x00"+raw.RuleID), UpstreamID: raw.RuleID,
			Source: model.SourceAgentGuard, FetchedAt: fetchedAt,
			RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: field},
		},
		Name: raw.Name, AgentID: agentID, AgentUpstreamID: agentUpstreamID, Scope: scope, Phase: "unknown",
		Action: raw.Action, Status: raw.Status, Severity: raw.Severity, Category: raw.Category,
		ToolPattern: raw.ToolPattern, Reason: boundedText(raw.Reason), UserManaged: *raw.UserManaged,
	}, nil
}

func normalizeCheckMessages(raw []rawCheckMessage, field string) ([]model.RuleCheckMessage, error) {
	items := make([]model.RuleCheckMessage, 0, len(raw))
	for index, message := range raw {
		if strings.TrimSpace(message.Message) == "" {
			return nil, &ContractError{Field: fmt.Sprintf("%s/%d/message", field, index), Problem: "required field is missing"}
		}
		items = append(items, model.RuleCheckMessage{Message: boundedText(message.Message)})
	}
	return items, nil
}

func normalizeApproval(raw rawApproval, field string, fetchedAt time.Time) (model.Approval, error) {
	if raw.TicketID == "" || raw.Created == nil || raw.Status != "pending" || raw.Event.EventType == "" || raw.Decision.Action == "" || raw.Decision.MatchedRules == nil {
		return model.Approval{}, &ContractError{Field: field, Problem: "pending ticket identity, event, and decision fields are required"}
	}
	createdAt := time.UnixMilli(*raw.Created).UTC()
	if raw.Event.Timestamp != nil && *raw.Event.Timestamp > 0 {
		createdAt = time.UnixMilli(*raw.Event.Timestamp).UTC()
	}
	agentUpstreamID := raw.Event.Principal.AgentID
	return model.Approval{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: stableID("approval", raw.TicketID), UpstreamID: raw.TicketID,
			Source: model.SourceAgentGuard, FetchedAt: fetchedAt,
			RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: field},
		},
		AgentID: opaqueAgentID(agentUpstreamID), AgentUpstreamID: agentUpstreamID,
		SessionID: raw.Event.Principal.SessionID, UserID: raw.Event.Principal.UserID,
		EventID: raw.Event.EventID, EventType: raw.Event.EventType, Tool: raw.Event.ToolCall.ToolName,
		Phase: eventPhase(raw.Event.EventType), Action: raw.Decision.Action,
		Reason: boundedText(raw.Decision.Reason), RiskScore: raw.Decision.RiskScore,
		MatchedRules: append([]string{}, raw.Decision.MatchedRules...), Status: raw.Status, CreatedAt: createdAt,
	}, nil
}

func eventPhase(eventType string) string {
	switch eventType {
	case "llm_input":
		return "LLM Before"
	case "llm_output":
		return "LLM After"
	case "tool_invoke":
		return "Tool Before"
	case "tool_result":
		return "Tool After"
	default:
		return "unknown"
	}
}

type explicitAgentIdentity struct {
	opaque   string
	upstream string
}

func explicitAgentIDs(snapshot model.TrustSnapshot) []explicitAgentIdentity {
	values := map[string]string{}
	for _, session := range snapshot.Sessions {
		if session.AgentUpstreamID != "" {
			values[session.AgentUpstreamID] = session.AgentID
		}
	}
	for _, resource := range snapshot.Resources {
		if resource.OwnerAgentUpstreamID != "" {
			values[resource.OwnerAgentUpstreamID] = resource.OwnerAgentID
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]explicitAgentIdentity, 0, len(keys))
	for _, key := range keys {
		items = append(items, explicitAgentIdentity{opaque: values[key], upstream: key})
	}
	return items
}

func normalizePluginConfig(raw rawPluginConfig, field, agentID string) (pluginConfigView, error) {
	if raw.AgentID != agentID {
		return pluginConfigView{}, &ContractError{Field: field + "/agent_id", Problem: "did not match the requested agent"}
	}
	if raw.ConfigSource != "agent_override" && raw.ConfigSource != "server_default" && raw.ConfigSource != "none" {
		return pluginConfigView{}, &ContractError{Field: field + "/config_source", Problem: "unsupported value"}
	}
	view := pluginConfigView{Source: raw.ConfigSource, Phases: map[string]pluginPhaseView{}}
	if len(raw.PluginConfig) == 0 || string(raw.PluginConfig) == "null" {
		return view, nil
	}
	var config struct {
		Phases map[string]struct {
			Client []json.RawMessage `json:"client"`
			Server []json.RawMessage `json:"server"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(raw.PluginConfig, &config); err != nil || config.Phases == nil {
		return pluginConfigView{}, &ContractError{Field: field + "/plugin_config/phases", Problem: "required object is missing"}
	}
	for phase, rawPhase := range config.Phases {
		if rawPhase.Client == nil || rawPhase.Server == nil {
			return pluginConfigView{}, &ContractError{Field: field + "/plugin_config/phases/" + phase, Problem: "client and server arrays are required"}
		}
		local, err := pluginNames(rawPhase.Client, field+"/plugin_config/phases/"+phase+"/client")
		if err != nil {
			return pluginConfigView{}, err
		}
		remote, err := pluginNames(rawPhase.Server, field+"/plugin_config/phases/"+phase+"/server")
		if err != nil {
			return pluginConfigView{}, err
		}
		view.Phases[phase] = pluginPhaseView{Local: local, Remote: remote}
	}
	return view, nil
}

func pluginNames(specs []json.RawMessage, field string) ([]string, error) {
	values := make([]string, 0, len(specs))
	for index, spec := range specs {
		var name string
		if json.Unmarshal(spec, &name) != nil || strings.TrimSpace(name) == "" {
			var object struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(spec, &object) != nil || strings.TrimSpace(object.Name) == "" {
				return nil, &ContractError{Field: fmt.Sprintf("%s/%d", field, index), Problem: "expected a plugin name or named object"}
			}
			name = object.Name
		}
		values = append(values, name)
	}
	sort.Strings(values)
	return values, nil
}

func normalizeAvailablePlugins(raw rawPluginAvailable, field, agentID string) (map[string][]string, map[string][]string, error) {
	if raw.AgentID != agentID {
		return nil, nil, &ContractError{Field: field + "/agent_id", Problem: "did not match the requested agent"}
	}
	if raw.LocalPlugins == nil || raw.RemotePlugins == nil {
		return nil, nil, &ContractError{Field: field, Problem: "local_plugins and remote_plugins arrays are required"}
	}
	local, err := availableByPhase(raw.LocalPlugins, field+"/local_plugins")
	if err != nil {
		return nil, nil, err
	}
	remote, err := availableByPhase(raw.RemotePlugins, field+"/remote_plugins")
	return local, remote, err
}

func availableByPhase(options []rawPluginOption, field string) (map[string][]string, error) {
	values := map[string][]string{}
	for index, option := range options {
		if option.Name == "" || option.Phases == nil {
			return nil, &ContractError{Field: fmt.Sprintf("%s/%d", field, index), Problem: "name and phases are required"}
		}
		for _, phase := range option.Phases {
			if strings.TrimSpace(phase) != "" {
				values[phase] = append(values[phase], option.Name)
			}
		}
	}
	for phase := range values {
		sort.Strings(values[phase])
	}
	return values, nil
}
