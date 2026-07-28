package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

var (
	ErrGatewayPolicyInvalidRequest  = errors.New("invalid agentgateway policy configuration request")
	ErrGatewayPolicyNotFound        = errors.New("agentgateway policy configuration resource not found")
	ErrGatewayPolicyWriteUnverified = errors.New("agentgateway policy configuration write could not be verified")
)

const (
	changeUpsertGatewayPolicy = "upsert-gateway-policy"
	changeDeleteGatewayPolicy = "delete-gateway-policy"
)

type gatewayPolicyCatalogEntry struct {
	Key         string
	Title       string
	Group       string
	Description string
	Phase       string
	Action      string
}

var llmGatewayPolicyCatalog = []gatewayPolicyCatalogEntry{
	{"cors", "CORS", "Access", "Handle CORS preflight requests and append configured CORS headers to applicable requests.", "Request", "Authorize"},
	{"apiKey", "API keys", "Access", "Authenticate incoming requests with API keys.", "Request", "Authenticate"},
	{"basicAuth", "Basic auth", "Access", "Authenticate incoming requests with Basic Auth credentials from an htpasswd user database.", "Request", "Authenticate"},
	{"jwtAuth", "JWT auth", "Access", "Authenticate incoming requests with JWT bearer tokens.", "Request", "Authenticate"},
	{"oidc", "OIDC", "Access", "Authenticate browser requests with OIDC authorization code flow.", "Request", "Authenticate"},
	{"authorization", "Authorization", "Access", "Apply authorization rules to incoming HTTP requests.", "Request", "Authorize"},
	{"extAuthz", "External authorization", "Access", "Authorize incoming requests by calling an external authorization service.", "Request", "Authorize"},
	{"guardrails", "Guardrails", "Safety", "Apply prompt and response guardrails to every configured model.", "Request + Response", "Inspect"},
	{"localRateLimit", "Local rate limit", "Traffic Shaping", "Apply local rate limits to incoming requests.", "Request", "Limit"},
	{"remoteRateLimit", "Remote rate limit", "Traffic Shaping", "Run remote rate-limit checks for incoming requests.", "Request", "Limit"},
	{"transformations", "Transformations", "Mutation", "Modify request and response headers, bodies, or metadata.", "Request + Response", "Transform"},
	{"extProc", "External processor", "Mutation", "Send request and response data to an external processing service.", "Request + Response", "Process"},
}

var modelPolicyCatalog = []gatewayPolicyCatalogEntry{
	{"authorization", "Authorization", "Access", "Apply HTTP authorization rules to requests for this direct model.", "Request", "Authorize"},
	{"defaults", "Request defaults", "Request Mutation", "Set default request-body values when the client omitted them.", "Request", "Transform"},
	{"overrides", "Request overrides", "Request Mutation", "Replace client-provided request-body values.", "Request", "Transform"},
	{"transformation", "Request transformation", "Request Mutation", "Compute request-body values from CEL expressions.", "Request", "Transform"},
	{"requestHeaders", "Request headers", "Request Mutation", "Modify headers sent to the LLM provider.", "Request", "Transform"},
	{"responseHeaders", "Response headers", "Response Mutation", "Modify headers returned from the LLM provider.", "Response", "Transform"},
	{"tls", "Backend TLS", "Backend", "Configure TLS when connecting to the LLM provider.", "Backend", "Secure"},
	{"backendTLS", "Backend TLS compatibility alias", "Backend", "Configure TLS through the verified compatibility alias.", "Backend", "Secure"},
	{"auth", "Backend authentication", "Backend", "Configure authentication sent to the LLM provider.", "Backend", "Authenticate"},
	{"health", "Backend health", "Backend", "Configure outlier detection for this model backend.", "Backend", "Monitor"},
	{"backendTunnel", "Backend tunnel", "Backend", "Configure tunneling when connecting to the LLM provider.", "Backend", "Tunnel"},
	{"guardrails", "Guardrails", "Safety", "Apply prompt or response guardrails to this direct model.", "Request + Response", "Inspect"},
	{"promptCaching", "Prompt caching", "Caching", "Configure cache-point insertion for supported LLM providers.", "Request", "Cache"},
}

var mcpGatewayPolicyCatalog = []gatewayPolicyCatalogEntry{
	{"mcpAuthentication", "MCP authentication", "MCP", "Authenticate MCP clients.", "Request", "Authenticate"},
	{"mcpAuthorization", "MCP authorization", "MCP", "Apply authorization rules to MCP requests.", "Request", "Authorize"},
	{"mcpGuardrails", "MCP guardrails", "MCP", "Apply external policy processors to MCP traffic.", "Request + Response", "Inspect"},
	{"authorization", "Authorization", "Access", "Apply authorization rules to incoming HTTP requests.", "Request", "Authorize"},
	{"cors", "CORS", "Access", "Handle CORS preflight requests and append configured CORS headers to applicable requests.", "Request", "Authorize"},
	{"extAuthz", "External authorization", "Access", "Authorize incoming requests by calling an external authorization service.", "Request", "Authorize"},
	{"jwtAuth", "JWT auth", "Access", "Authenticate incoming requests with JWT bearer tokens.", "Request", "Authenticate"},
	{"localRateLimit", "Local rate limit", "Traffic Shaping", "Apply local rate limits to incoming requests.", "Request", "Limit"},
	{"remoteRateLimit", "Remote rate limit", "Traffic Shaping", "Run remote rate-limit checks for incoming requests.", "Request", "Limit"},
	{"transformations", "Transformations", "Mutation", "Modify request and response headers, bodies, or metadata.", "Request + Response", "Transform"},
	{"extProc", "External processor", "Mutation", "Send request and response data to an external processing service.", "Request + Response", "Process"},
}

type gatewayPolicyConfigDocument struct {
	root map[string]json.RawMessage
}

type gatewayPolicyLocation struct {
	setting    model.GatewayPolicySetting
	family     string
	key        string
	modelIndex int
}

func (client *Client) PolicyConfiguration(ctx context.Context) (model.GatewayPolicyConfiguration, string, error) {
	document, revision, err := client.readPolicyDocument(ctx)
	if err != nil {
		return model.GatewayPolicyConfiguration{}, "", err
	}
	configuration, err := document.configuration(time.Now().UTC())
	return configuration, revision, err
}

func (client *Client) ApplyPolicyChange(ctx context.Context, expectedRevision string, change model.GatewayPolicyChange) (string, error) {
	document, revision, err := client.readPolicyDocument(ctx)
	if err != nil {
		return "", err
	}
	if revision != expectedRevision {
		return "", ErrConfigurationChanged
	}
	location, err := document.policyByID(change.ResourceID)
	if err != nil {
		return "", err
	}
	if err := document.applyPolicyChange(location, change); err != nil {
		return "", err
	}
	expected, err := document.marshal()
	if err != nil {
		return "", &ContractError{Field: "/api/config", Problem: "configuration could not be encoded"}
	}
	var response struct {
		Status string `json:"status"`
	}
	if _, err := client.upstream.PostMutationJSON(ctx, "/api/config", json.RawMessage(expected), &response); err != nil {
		return "", err
	}
	if response.Status != "success" {
		return "", &ContractError{Field: "/api/config/status", Problem: "write success was not confirmed"}
	}
	actual, _, err := client.readPolicyDocument(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrGatewayPolicyWriteUnverified, err)
	}
	actualPayload, err := actual.marshal()
	if err != nil || !bytes.Equal(expected, actualPayload) {
		return "", ErrGatewayPolicyWriteUnverified
	}
	return location.setting.RawRef.ID, nil
}

func (client *Client) readPolicyDocument(ctx context.Context) (*gatewayPolicyConfigDocument, string, error) {
	var payload json.RawMessage
	if _, err := client.upstream.GetJSON(ctx, "/api/config", &payload); err != nil {
		return nil, "", err
	}
	document, err := decodeGatewayPolicyDocument(payload)
	if err != nil {
		return nil, "", err
	}
	canonical, err := document.marshal()
	if err != nil {
		return nil, "", &ContractError{Field: "/api/config", Problem: "configuration could not be normalized"}
	}
	digest := sha256.Sum256(canonical)
	return document, hex.EncodeToString(digest[:]), nil
}

func decodeGatewayPolicyDocument(payload json.RawMessage) (*gatewayPolicyConfigDocument, error) {
	root := make(map[string]json.RawMessage)
	if err := json.Unmarshal(payload, &root); err != nil || root == nil {
		return nil, &ContractError{Field: "/api/config", Problem: "expected configuration object"}
	}
	return &gatewayPolicyConfigDocument{root: root}, nil
}

func (document *gatewayPolicyConfigDocument) marshal() ([]byte, error) {
	payload, err := json.Marshal(document.root)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (document *gatewayPolicyConfigDocument) configuration(fetchedAt time.Time) (model.GatewayPolicyConfiguration, error) {
	locations, err := document.policyLocations(fetchedAt)
	if err != nil {
		return model.GatewayPolicyConfiguration{}, err
	}
	policies := make([]model.GatewayPolicySetting, 0, len(locations))
	for _, location := range locations {
		policies = append(policies, location.setting)
	}
	return model.GatewayPolicyConfiguration{
		Source: model.SourceAgentGateway, FetchedAt: fetchedAt, Settings: policies,
	}, nil
}

func (document *gatewayPolicyConfigDocument) policyLocations(fetchedAt time.Time) ([]gatewayPolicyLocation, error) {
	llm, _, err := optionalPolicyObject(document.root["llm"], "/llm")
	if err != nil {
		return nil, err
	}
	llmPolicies, _, err := optionalPolicyObject(llm["policies"], "/llm/policies")
	if err != nil {
		return nil, err
	}
	locations, err := catalogPolicyLocations("llm", "LLM Gateway", "Gateway", "/llm/policies", llmGatewayPolicyCatalog, llmPolicies, -1, fetchedAt)
	if err != nil {
		return nil, err
	}

	var models []json.RawMessage
	if err := decodeArray(llm["models"], "/llm/models", &models); err != nil {
		return nil, err
	}
	for index, rawModel := range models {
		field := fmt.Sprintf("/llm/models/%d", index)
		modelObject, err := requiredPolicyObject(rawModel, field)
		if err != nil {
			return nil, err
		}
		name := optionalString(modelObject["name"], "")
		if strings.TrimSpace(name) == "" {
			return nil, &ContractError{Field: field + "/name", Problem: "expected non-empty string"}
		}
		modelLocations, err := catalogPolicyLocations("model", name, "Model", field, modelPolicyCatalog, modelObject, index, fetchedAt)
		if err != nil {
			return nil, err
		}
		locations = append(locations, modelLocations...)
	}

	mcp, _, err := optionalPolicyObject(document.root["mcp"], "/mcp")
	if err != nil {
		return nil, err
	}
	mcpPolicies, _, err := optionalPolicyObject(mcp["policies"], "/mcp/policies")
	if err != nil {
		return nil, err
	}
	mcpLocations, err := catalogPolicyLocations("mcp", "MCP Gateway", "Gateway", "/mcp/policies", mcpGatewayPolicyCatalog, mcpPolicies, -1, fetchedAt)
	if err != nil {
		return nil, err
	}
	return append(locations, mcpLocations...), nil
}

func catalogPolicyLocations(family, target, scope, basePath string, catalog []gatewayPolicyCatalogEntry, values map[string]json.RawMessage, modelIndex int, fetchedAt time.Time) ([]gatewayPolicyLocation, error) {
	locations := make([]gatewayPolicyLocation, 0, len(catalog)+len(values))
	known := make(map[string]struct{}, len(catalog))
	for _, entry := range catalog {
		known[entry.Key] = struct{}{}
		location, err := policyLocation(family, target, scope, basePath, entry, values[entry.Key], true, modelIndex, fetchedAt)
		if err != nil {
			return nil, err
		}
		locations = append(locations, location)
	}
	if family == "model" {
		return locations, nil
	}
	unknown := make([]string, 0)
	for key := range values {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		entry := gatewayPolicyCatalogEntry{
			Key: key, Title: titleFromPolicyKey(key), Group: "Other",
			Description: "Existing source-owned policy outside the verified editable catalog.",
			Phase:       "Unknown", Action: "Preserve",
		}
		location, err := policyLocation(family, target, scope, basePath, entry, values[key], false, modelIndex, fetchedAt)
		if err != nil {
			return nil, err
		}
		locations = append(locations, location)
	}
	return locations, nil
}

func policyLocation(family, target, scope, basePath string, entry gatewayPolicyCatalogEntry, raw json.RawMessage, editable bool, modelIndex int, fetchedAt time.Time) (gatewayPolicyLocation, error) {
	path := basePath + "/" + escapeJSONPointer(entry.Key)
	value, err := policyJSONValue(raw, path)
	if err != nil {
		return gatewayPolicyLocation{}, err
	}
	setting := model.GatewayPolicySetting{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: base64.RawURLEncoding.EncodeToString([]byte(path)), UpstreamID: path,
			Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: path},
		},
		Family: family, Target: target, Key: entry.Key, Title: entry.Title, Group: entry.Group,
		Description: entry.Description, Scope: scope, Phase: entry.Phase, Action: entry.Action,
		Enabled: policyRawEnabled(raw), Editable: editable, Value: value,
	}
	return gatewayPolicyLocation{setting: setting, family: family, key: entry.Key, modelIndex: modelIndex}, nil
}

func (document *gatewayPolicyConfigDocument) policyByID(resourceID string) (gatewayPolicyLocation, error) {
	if strings.TrimSpace(resourceID) == "" {
		return gatewayPolicyLocation{}, ErrGatewayPolicyInvalidRequest
	}
	locations, err := document.policyLocations(time.Time{})
	if err != nil {
		return gatewayPolicyLocation{}, err
	}
	for _, location := range locations {
		if location.setting.ID == resourceID {
			return location, nil
		}
	}
	return gatewayPolicyLocation{}, ErrGatewayPolicyNotFound
}

func (document *gatewayPolicyConfigDocument) applyPolicyChange(location gatewayPolicyLocation, change model.GatewayPolicyChange) error {
	if !location.setting.Editable {
		return ErrGatewayPolicyInvalidRequest
	}
	switch change.Operation {
	case changeUpsertGatewayPolicy:
		value, err := encodePolicyValue(change.Value)
		if err != nil {
			return err
		}
		return document.setPolicy(location, value)
	case changeDeleteGatewayPolicy:
		if !location.setting.Enabled {
			return ErrGatewayPolicyNotFound
		}
		return document.deletePolicy(location)
	default:
		return ErrGatewayPolicyInvalidRequest
	}
}

func (document *gatewayPolicyConfigDocument) setPolicy(location gatewayPolicyLocation, value json.RawMessage) error {
	switch location.family {
	case "llm":
		return document.setGlobalPolicy("llm", "models", location.key, value)
	case "mcp":
		return document.setGlobalPolicy("mcp", "targets", location.key, value)
	case "model":
		return document.setModelPolicy(location.modelIndex, location.key, value)
	default:
		return ErrGatewayPolicyInvalidRequest
	}
}

func (document *gatewayPolicyConfigDocument) deletePolicy(location gatewayPolicyLocation) error {
	switch location.family {
	case "llm":
		return document.disableLLMPolicy(location.key)
	case "mcp":
		return document.deleteGlobalPolicy(location.family, location.key)
	case "model":
		return document.deleteModelPolicy(location.modelIndex, location.key)
	default:
		return ErrGatewayPolicyInvalidRequest
	}
}

func (document *gatewayPolicyConfigDocument) disableLLMPolicy(key string) error {
	owner, present, err := optionalPolicyObject(document.root["llm"], "/llm")
	if err != nil || !present {
		return err
	}
	policies, policyPresent, err := optionalPolicyObject(owner["policies"], "/llm/policies")
	if err != nil || !policyPresent {
		return err
	}
	if key == "localRateLimit" {
		delete(policies, key)
	} else {
		policies[key] = json.RawMessage("null")
	}
	return document.storeGlobalPolicies("llm", owner, policies)
}

func (document *gatewayPolicyConfigDocument) setGlobalPolicy(section, requiredArray, key string, value json.RawMessage) error {
	sectionPath := "/" + section
	owner, present, err := optionalPolicyObject(document.root[section], sectionPath)
	if err != nil {
		return err
	}
	if !present {
		owner = make(map[string]json.RawMessage)
	}
	if raw := owner[requiredArray]; len(raw) == 0 || string(raw) == "null" {
		owner[requiredArray] = json.RawMessage("[]")
	}
	policies, _, err := optionalPolicyObject(owner["policies"], sectionPath+"/policies")
	if err != nil {
		return err
	}
	policies[key] = value
	encodedPolicies, err := json.Marshal(policies)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	owner["policies"] = encodedPolicies
	encodedOwner, err := json.Marshal(owner)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	document.root[section] = encodedOwner
	return nil
}

func (document *gatewayPolicyConfigDocument) deleteGlobalPolicy(section, key string) error {
	sectionPath := "/" + section
	owner, present, err := optionalPolicyObject(document.root[section], sectionPath)
	if err != nil || !present {
		return err
	}
	policies, policyPresent, err := optionalPolicyObject(owner["policies"], sectionPath+"/policies")
	if err != nil || !policyPresent {
		return err
	}
	delete(policies, key)
	return document.storeGlobalPolicies(section, owner, policies)
}

func (document *gatewayPolicyConfigDocument) storeGlobalPolicies(section string, owner, policies map[string]json.RawMessage) error {
	encodedPolicies, err := json.Marshal(policies)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	owner["policies"] = encodedPolicies
	encodedOwner, err := json.Marshal(owner)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	document.root[section] = encodedOwner
	return nil
}

func (document *gatewayPolicyConfigDocument) setModelPolicy(index int, key string, value json.RawMessage) error {
	llm, models, err := document.mutableModels()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(models) {
		return ErrGatewayPolicyNotFound
	}
	field := fmt.Sprintf("/llm/models/%d", index)
	modelObject, err := requiredPolicyObject(models[index], field)
	if err != nil {
		return err
	}
	if key == "tls" {
		if _, configured := modelObject["backendTLS"]; configured {
			return ErrGatewayPolicyInvalidRequest
		}
	}
	if key == "backendTLS" {
		if _, configured := modelObject["tls"]; configured {
			return ErrGatewayPolicyInvalidRequest
		}
	}
	modelObject[key] = value
	models[index], err = json.Marshal(modelObject)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	return document.storeModels(llm, models)
}

func (document *gatewayPolicyConfigDocument) deleteModelPolicy(index int, key string) error {
	llm, models, err := document.mutableModels()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(models) {
		return ErrGatewayPolicyNotFound
	}
	field := fmt.Sprintf("/llm/models/%d", index)
	modelObject, err := requiredPolicyObject(models[index], field)
	if err != nil {
		return err
	}
	delete(modelObject, key)
	models[index], err = json.Marshal(modelObject)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	return document.storeModels(llm, models)
}

func (document *gatewayPolicyConfigDocument) mutableModels() (map[string]json.RawMessage, []json.RawMessage, error) {
	llm, present, err := optionalPolicyObject(document.root["llm"], "/llm")
	if err != nil {
		return nil, nil, err
	}
	if !present {
		return nil, nil, ErrGatewayPolicyNotFound
	}
	var models []json.RawMessage
	if err := decodeArray(llm["models"], "/llm/models", &models); err != nil {
		return nil, nil, err
	}
	return llm, models, nil
}

func (document *gatewayPolicyConfigDocument) storeModels(llm map[string]json.RawMessage, models []json.RawMessage) error {
	encodedModels, err := json.Marshal(models)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	llm["models"] = encodedModels
	encodedLLM, err := json.Marshal(llm)
	if err != nil {
		return ErrGatewayPolicyInvalidRequest
	}
	document.root["llm"] = encodedLLM
	return nil
}

func optionalPolicyObject(raw json.RawMessage, field string) (map[string]json.RawMessage, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return make(map[string]json.RawMessage), false, nil
	}
	value := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, false, &ContractError{Field: field, Problem: "expected object"}
	}
	return value, true, nil
}

func requiredPolicyObject(raw json.RawMessage, field string) (map[string]json.RawMessage, error) {
	value := make(map[string]json.RawMessage)
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, &ContractError{Field: field, Problem: "expected object"}
	}
	return value, nil
}

func policyJSONValue(raw json.RawMessage, field string) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, &ContractError{Field: field, Problem: "expected JSON value"}
	}
	return json.RawMessage(append([]byte(nil), raw...)), nil
}

func policyRawEnabled(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil {
		return len(values) > 0
	}
	return true
}

func encodePolicyValue(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, ErrGatewayPolicyInvalidRequest
	}
	encoded, err := json.Marshal(value)
	if err != nil || bytes.Equal(encoded, []byte("null")) {
		return nil, ErrGatewayPolicyInvalidRequest
	}
	return encoded, nil
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func titleFromPolicyKey(value string) string {
	if value == "" {
		return "Unknown policy"
	}
	var result strings.Builder
	for index, character := range value {
		if index > 0 && unicode.IsUpper(character) {
			result.WriteByte(' ')
		}
		if index == 0 {
			character = unicode.ToUpper(character)
		}
		result.WriteRune(character)
	}
	return result.String()
}
