package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

var (
	ErrMCPInvalidRequest   = errors.New("invalid MCP configuration request")
	ErrMCPResourceNotFound = errors.New("MCP configuration resource not found")
	ErrMCPResourceConflict = errors.New("MCP configuration resource conflict")
	ErrMCPWriteUnverified  = errors.New("agentgateway MCP configuration write could not be verified")
)

const (
	changeUpdateMCPSettings = "update-mcp-settings"
	changeCreateMCPServer   = "create-mcp-server"
	changeUpdateMCPServer   = "update-mcp-server"
	changeDeleteMCPServer   = "delete-mcp-server"
)

var mcpTargetName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

type mcpConfigDocument struct {
	root    map[string]json.RawMessage
	mcp     map[string]json.RawMessage
	targets []json.RawMessage
}

func (client *Client) MCPConfiguration(ctx context.Context) (model.MCPConfiguration, string, error) {
	payload, document, revision, err := client.readMCPDocument(ctx)
	if err != nil {
		return model.MCPConfiguration{}, "", err
	}
	fetchedAt := time.Now().UTC()
	configuration, err := document.configuration(fetchedAt)
	if err != nil {
		return model.MCPConfiguration{}, "", err
	}
	configuration.InlineServers, err = inlineMCPServers(payload, fetchedAt)
	if err != nil {
		return model.MCPConfiguration{}, "", err
	}
	return configuration, revision, nil
}

func (client *Client) ApplyMCPChange(ctx context.Context, expectedRevision string, change model.MCPChange) (string, error) {
	_, document, revision, err := client.readMCPDocument(ctx)
	if err != nil {
		return "", err
	}
	if revision != expectedRevision {
		return "", ErrConfigurationChanged
	}
	target, err := document.changeTarget(change)
	if err != nil {
		return "", err
	}
	if err := document.apply(change); err != nil {
		return "", err
	}
	updated, err := document.marshal()
	if err != nil {
		return "", &ContractError{Field: "/api/config", Problem: "configuration could not be encoded"}
	}
	var response struct {
		Status string `json:"status"`
	}
	if _, err := client.upstream.PostMutationJSON(ctx, "/api/config", json.RawMessage(updated), &response); err != nil {
		return "", err
	}
	if response.Status != "success" {
		return "", &ContractError{Field: "/api/config/status", Problem: "write success was not confirmed"}
	}
	configuration, _, err := client.MCPConfiguration(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMCPWriteUnverified, err)
	}
	if !mcpChangeVerified(configuration, change, target) {
		return "", ErrMCPWriteUnverified
	}
	return target, nil
}

func (client *Client) readMCPDocument(ctx context.Context) (json.RawMessage, *mcpConfigDocument, string, error) {
	var payload json.RawMessage
	if _, err := client.upstream.GetJSON(ctx, "/api/config", &payload); err != nil {
		return nil, nil, "", err
	}
	document, err := decodeMCPDocument(payload)
	if err != nil {
		return nil, nil, "", err
	}
	canonical, err := document.marshal()
	if err != nil {
		return nil, nil, "", &ContractError{Field: "/api/config", Problem: "configuration could not be normalized"}
	}
	digest := sha256.Sum256(canonical)
	return payload, document, hex.EncodeToString(digest[:]), nil
}

func decodeMCPDocument(payload json.RawMessage) (*mcpConfigDocument, error) {
	root := make(map[string]json.RawMessage)
	if err := json.Unmarshal(payload, &root); err != nil || root == nil {
		return nil, &ContractError{Field: "/api/config", Problem: "expected configuration object"}
	}
	mcp := make(map[string]json.RawMessage)
	if raw := root["mcp"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &mcp); err != nil || mcp == nil {
			return nil, &ContractError{Field: "/mcp", Problem: "expected object"}
		}
	}
	document := &mcpConfigDocument{root: root, mcp: mcp}
	if err := decodeArray(mcp["targets"], "/mcp/targets", &document.targets); err != nil {
		return nil, err
	}
	return document, nil
}

func (document *mcpConfigDocument) marshal() ([]byte, error) {
	targets, err := json.Marshal(document.targets)
	if err != nil {
		return nil, err
	}
	document.mcp["targets"] = targets
	mcp, err := json.Marshal(document.mcp)
	if err != nil {
		return nil, err
	}
	document.root["mcp"] = mcp
	return json.Marshal(document.root)
}

func (document *mcpConfigDocument) configuration(fetchedAt time.Time) (model.MCPConfiguration, error) {
	settings, err := document.settings()
	if err != nil {
		return model.MCPConfiguration{}, err
	}
	configuration := model.MCPConfiguration{
		Source: model.SourceAgentGateway, FetchedAt: fetchedAt, Settings: settings,
		Servers: []model.MCPServerSetting{}, InlineServers: []model.GatewayMCPServer{},
	}
	for index, raw := range document.targets {
		setting, err := mcpServerSetting(raw, fmt.Sprintf("/mcp/targets/%d", index), fetchedAt)
		if err != nil {
			return model.MCPConfiguration{}, err
		}
		configuration.Servers = append(configuration.Servers, setting)
	}
	return configuration, nil
}

func (document *mcpConfigDocument) settings() (model.MCPGlobalSettings, error) {
	settings := model.MCPGlobalSettings{StatefulMode: "stateless", PrefixMode: "none", FailureMode: "failClosed"}
	if raw := document.mcp["port"]; len(raw) > 0 && string(raw) != "null" {
		var port int
		if json.Unmarshal(raw, &port) != nil || port < 0 || port > 65535 {
			return model.MCPGlobalSettings{}, &ContractError{Field: "/mcp/port", Problem: "expected a valid port or null"}
		}
		settings.Port = &port
	}
	if err := readMCPEnum(document.mcp, "statefulMode", "/mcp/statefulMode", &settings.StatefulMode, "stateless", "stateful"); err != nil {
		return model.MCPGlobalSettings{}, err
	}
	if raw := document.mcp["prefixMode"]; len(raw) > 0 && string(raw) != "null" {
		if err := readMCPEnum(document.mcp, "prefixMode", "/mcp/prefixMode", &settings.PrefixMode, "always", "conditional"); err != nil {
			return model.MCPGlobalSettings{}, err
		}
	}
	if raw := document.mcp["failureMode"]; len(raw) > 0 && string(raw) != "null" {
		if err := readMCPEnum(document.mcp, "failureMode", "/mcp/failureMode", &settings.FailureMode, "failClosed", "failOpen"); err != nil {
			return model.MCPGlobalSettings{}, err
		}
	}
	settings.HasPolicies = rawConfigured(document.mcp["policies"])
	return settings, nil
}

func readMCPEnum(values map[string]json.RawMessage, key, field string, destination *string, allowed ...string) error {
	raw := values[key]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return &ContractError{Field: field, Problem: "expected a verified enum value"}
	}
	for _, candidate := range allowed {
		if value == candidate {
			*destination = value
			return nil
		}
	}
	return &ContractError{Field: field, Problem: "expected a verified enum value"}
}

func mcpServerSetting(raw json.RawMessage, field string, fetchedAt time.Time) (model.MCPServerSetting, error) {
	item := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &item); err != nil || item == nil {
		return model.MCPServerSetting{}, &ContractError{Field: field, Problem: "expected object"}
	}
	var name string
	if json.Unmarshal(item["name"], &name) != nil || name == "" {
		return model.MCPServerSetting{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
	}
	transports := make([]string, 0, 1)
	for _, transport := range []string{"sse", "mcp", "stdio", "openapi"} {
		if rawConfigured(item[transport]) {
			transports = append(transports, transport)
		}
	}
	if len(transports) != 1 {
		return model.MCPServerSetting{}, &ContractError{Field: field, Problem: "expected exactly one verified transport"}
	}
	setting := model.MCPServerSetting{
		ConnectResource: resource(field, name, fetchedAt), Name: name, Transport: transports[0], Scope: "gateway",
		HasPolicies: rawConfigured(item["policies"]), Editable: transports[0] != "openapi",
	}
	switch setting.Transport {
	case "sse", "mcp", "openapi":
		network, err := decodeMCPNetwork(item[setting.Transport], field+"/"+setting.Transport)
		if err != nil {
			return model.MCPServerSetting{}, err
		}
		setting.Network = &network
	case "stdio":
		stdio, err := decodeMCPStdio(item["stdio"], field+"/stdio")
		if err != nil {
			return model.MCPServerSetting{}, err
		}
		setting.Stdio = &stdio
	}
	return setting, nil
}

func decodeMCPNetwork(raw json.RawMessage, field string) (model.MCPNetworkTarget, error) {
	var value struct {
		Host    string `json:"host"`
		Port    *int   `json:"port"`
		Path    string `json:"path"`
		Backend string `json:"backend"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return model.MCPNetworkTarget{}, &ContractError{Field: field, Problem: "expected network target object"}
	}
	switch {
	case value.Backend != "" && value.Host == "" && value.Port == nil:
		return model.MCPNetworkTarget{Mode: "backend", Backend: value.Backend, Path: value.Path}, nil
	case value.Host != "" && value.Backend == "" && value.Port == nil && value.Path == "":
		return model.MCPNetworkTarget{Mode: "url", Host: value.Host}, nil
	case value.Host != "" && value.Backend == "" && value.Port != nil && value.Path != "" && *value.Port >= 0 && *value.Port <= 65535:
		return model.MCPNetworkTarget{Mode: "host", Host: value.Host, Port: value.Port, Path: value.Path}, nil
	default:
		return model.MCPNetworkTarget{}, &ContractError{Field: field, Problem: "expected verified URL, host/port/path, or backend/path target"}
	}
}

func decodeMCPStdio(raw json.RawMessage, field string) (model.MCPStdioTarget, error) {
	var value struct {
		Command          string            `json:"cmd"`
		Arguments        []string          `json:"args"`
		Environment      map[string]string `json:"env"`
		ClearEnvironment bool              `json:"clear_env"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Command == "" {
		return model.MCPStdioTarget{}, &ContractError{Field: field, Problem: "expected verified stdio target"}
	}
	if value.Arguments == nil {
		value.Arguments = []string{}
	}
	if value.Environment == nil {
		value.Environment = map[string]string{}
	}
	return model.MCPStdioTarget(value), nil
}

func (document *mcpConfigDocument) changeTarget(change model.MCPChange) (string, error) {
	switch change.Operation {
	case changeUpdateMCPSettings:
		return "MCP settings", nil
	case changeCreateMCPServer, changeUpdateMCPServer:
		return change.Server.Name, nil
	case changeDeleteMCPServer:
		_, raw, err := document.targetByID(change.ResourceID)
		if err != nil {
			return "", err
		}
		setting, err := mcpServerSetting(raw, "/mcp/targets", time.Time{})
		if err != nil {
			return "", ErrMCPInvalidRequest
		}
		return setting.Name, nil
	default:
		return "", ErrMCPInvalidRequest
	}
}

func (document *mcpConfigDocument) apply(change model.MCPChange) error {
	switch change.Operation {
	case changeUpdateMCPSettings:
		return document.applySettings(change.Settings)
	case changeCreateMCPServer:
		if err := validateMCPServerDraft(change.Server); err != nil {
			return err
		}
		if document.targetNameExists(change.Server.Name, -1) {
			return ErrMCPResourceConflict
		}
		raw, err := encodeMCPServer(change.Server, nil)
		if err != nil {
			return err
		}
		document.targets = append(document.targets, raw)
		return nil
	case changeUpdateMCPServer:
		if err := validateMCPServerDraft(change.Server); err != nil {
			return err
		}
		index, current, err := document.targetByID(change.ResourceID)
		if err != nil {
			return err
		}
		setting, err := mcpServerSetting(current, fmt.Sprintf("/mcp/targets/%d", index), time.Time{})
		if err != nil || !setting.Editable {
			return ErrMCPInvalidRequest
		}
		if document.targetNameExists(change.Server.Name, index) {
			return ErrMCPResourceConflict
		}
		raw, err := encodeMCPServer(change.Server, current)
		if err != nil {
			return err
		}
		document.targets[index] = raw
		return nil
	case changeDeleteMCPServer:
		index, current, err := document.targetByID(change.ResourceID)
		if err != nil {
			return err
		}
		setting, err := mcpServerSetting(current, fmt.Sprintf("/mcp/targets/%d", index), time.Time{})
		if err != nil || !setting.Editable {
			return ErrMCPInvalidRequest
		}
		document.targets = append(document.targets[:index], document.targets[index+1:]...)
		return nil
	default:
		return ErrMCPInvalidRequest
	}
}

func (document *mcpConfigDocument) applySettings(settings model.MCPGlobalSettingsDraft) error {
	if err := validateMCPSettings(settings); err != nil {
		return err
	}
	port, _ := json.Marshal(settings.Port)
	statefulMode, _ := json.Marshal(settings.StatefulMode)
	failureMode, _ := json.Marshal(settings.FailureMode)
	document.mcp["port"] = port
	document.mcp["statefulMode"] = statefulMode
	document.mcp["failureMode"] = failureMode
	if settings.PrefixMode == "none" {
		document.mcp["prefixMode"] = json.RawMessage("null")
	} else {
		prefixMode, _ := json.Marshal(settings.PrefixMode)
		document.mcp["prefixMode"] = prefixMode
	}
	return nil
}

func (document *mcpConfigDocument) targetByID(id string) (int, json.RawMessage, error) {
	for index, raw := range document.targets {
		field := fmt.Sprintf("/mcp/targets/%d", index)
		if resource(field, "", time.Time{}).ID == id {
			return index, raw, nil
		}
	}
	return -1, nil, ErrMCPResourceNotFound
}

func (document *mcpConfigDocument) targetNameExists(name string, except int) bool {
	for index, raw := range document.targets {
		if index == except {
			continue
		}
		var item struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Name == name {
			return true
		}
	}
	return false
}

func validateMCPSettings(settings model.MCPGlobalSettingsDraft) error {
	if settings.Port != nil && (*settings.Port < 0 || *settings.Port > 65535) {
		return ErrMCPInvalidRequest
	}
	if settings.StatefulMode != "stateless" && settings.StatefulMode != "stateful" {
		return ErrMCPInvalidRequest
	}
	if settings.PrefixMode != "none" && settings.PrefixMode != "always" && settings.PrefixMode != "conditional" {
		return ErrMCPInvalidRequest
	}
	if settings.FailureMode != "failClosed" && settings.FailureMode != "failOpen" {
		return ErrMCPInvalidRequest
	}
	return nil
}

func validateMCPServerDraft(draft model.MCPServerDraft) error {
	if len(draft.Name) > 253 || !mcpTargetName.MatchString(draft.Name) {
		return ErrMCPInvalidRequest
	}
	switch draft.Transport {
	case "mcp", "sse":
		if draft.Network == nil || draft.Stdio != nil || validateMCPNetwork(*draft.Network) != nil {
			return ErrMCPInvalidRequest
		}
	case "stdio":
		if draft.Stdio == nil || draft.Network != nil || validateMCPStdio(*draft.Stdio) != nil {
			return ErrMCPInvalidRequest
		}
	default:
		return ErrMCPInvalidRequest
	}
	return nil
}

func validateMCPNetwork(network model.MCPNetworkTarget) error {
	if len(network.Host) > 2048 || len(network.Path) > 2048 || len(network.Backend) > 256 ||
		strings.ContainsRune(network.Host+network.Path+network.Backend, '\x00') {
		return ErrMCPInvalidRequest
	}
	switch network.Mode {
	case "url":
		parsed, err := url.Parse(network.Host)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
			network.Port != nil || network.Path != "" || network.Backend != "" {
			return ErrMCPInvalidRequest
		}
	case "host":
		if network.Host == "" || network.Port == nil || *network.Port < 0 || *network.Port > 65535 ||
			network.Path == "" || network.Backend != "" {
			return ErrMCPInvalidRequest
		}
	case "backend":
		if network.Backend == "" || network.Host != "" || network.Port != nil {
			return ErrMCPInvalidRequest
		}
	default:
		return ErrMCPInvalidRequest
	}
	return nil
}

func validateMCPStdio(stdio model.MCPStdioTarget) error {
	if stdio.Command == "" || len(stdio.Command) > 4096 || strings.ContainsRune(stdio.Command, '\x00') ||
		len(stdio.Arguments) > 256 || len(stdio.Environment) > 256 {
		return ErrMCPInvalidRequest
	}
	for _, argument := range stdio.Arguments {
		if len(argument) > 4096 || strings.ContainsRune(argument, '\x00') {
			return ErrMCPInvalidRequest
		}
	}
	for key, value := range stdio.Environment {
		if key == "" || len(key) > 512 || len(value) > 16384 || strings.ContainsRune(key+value, '\x00') {
			return ErrMCPInvalidRequest
		}
	}
	return nil
}

func encodeMCPServer(draft model.MCPServerDraft, current json.RawMessage) (json.RawMessage, error) {
	item := make(map[string]json.RawMessage)
	if len(current) > 0 && json.Unmarshal(current, &item) != nil {
		return nil, ErrMCPInvalidRequest
	}
	for _, transport := range []string{"sse", "mcp", "stdio", "openapi"} {
		delete(item, transport)
	}
	name, _ := json.Marshal(draft.Name)
	item["name"] = name
	if draft.Transport == "stdio" {
		value := struct {
			Command          string            `json:"cmd"`
			Arguments        []string          `json:"args"`
			Environment      map[string]string `json:"env"`
			ClearEnvironment bool              `json:"clear_env"`
		}{draft.Stdio.Command, draft.Stdio.Arguments, draft.Stdio.Environment, draft.Stdio.ClearEnvironment}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		item["stdio"] = raw
	} else {
		value := make(map[string]any)
		switch draft.Network.Mode {
		case "url":
			value["host"] = draft.Network.Host
		case "host":
			value["host"], value["port"], value["path"] = draft.Network.Host, *draft.Network.Port, draft.Network.Path
		case "backend":
			value["backend"] = draft.Network.Backend
			if draft.Network.Path != "" {
				value["path"] = draft.Network.Path
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		item[draft.Transport] = raw
	}
	return json.Marshal(item)
}

func mcpChangeVerified(configuration model.MCPConfiguration, change model.MCPChange, target string) bool {
	switch change.Operation {
	case changeUpdateMCPSettings:
		settings := configuration.Settings
		return reflect.DeepEqual(settings.Port, change.Settings.Port) && settings.StatefulMode == change.Settings.StatefulMode &&
			settings.PrefixMode == change.Settings.PrefixMode && settings.FailureMode == change.Settings.FailureMode
	case changeCreateMCPServer, changeUpdateMCPServer:
		for _, setting := range configuration.Servers {
			if setting.Name == target {
				return mcpServerMatchesDraft(setting, change.Server)
			}
		}
		return false
	case changeDeleteMCPServer:
		for _, setting := range configuration.Servers {
			if setting.Name == target {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func mcpServerMatchesDraft(setting model.MCPServerSetting, draft model.MCPServerDraft) bool {
	if setting.Name != draft.Name || setting.Transport != draft.Transport {
		return false
	}
	if draft.Transport == "stdio" {
		return setting.Network == nil && reflect.DeepEqual(setting.Stdio, draft.Stdio)
	}
	return setting.Stdio == nil && reflect.DeepEqual(setting.Network, draft.Network)
}

func inlineMCPServers(payload json.RawMessage, fetchedAt time.Time) ([]model.GatewayMCPServer, error) {
	var config rawConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return nil, &ContractError{Field: "/api/config", Problem: "unexpected field type"}
	}
	servers := []model.GatewayMCPServer{}
	for bindIndex, bind := range config.Binds {
		for listenerIndex, listener := range bind.Listeners {
			routeGroups := []struct {
				name   string
				routes []rawRoute
			}{{"routes", listener.Routes}, {"tcpRoutes", listener.TCPRoutes}}
			for _, group := range routeGroups {
				for routeIndex, route := range group.routes {
					routeField := fmt.Sprintf("/binds/%d/listeners/%d/%s/%d", bindIndex, listenerIndex, group.name, routeIndex)
					for backendIndex, backend := range route.Backends {
						if backend.MCP == nil {
							continue
						}
						for targetIndex, item := range backend.MCP.Targets {
							field := fmt.Sprintf("%s/backends/%d/mcp/targets/%d", routeField, backendIndex, targetIndex)
							server, err := mcpResource(item, field, routeField, fetchedAt)
							if err != nil {
								return nil, err
							}
							servers = append(servers, server)
						}
					}
				}
			}
		}
	}
	return servers, nil
}
