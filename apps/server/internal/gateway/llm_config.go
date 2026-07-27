package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

var (
	ErrConfigurationChanged  = errors.New("agentgateway configuration changed")
	ErrLLMInvalidRequest     = errors.New("invalid LLM configuration request")
	ErrLLMResourceNotFound   = errors.New("LLM configuration resource not found")
	ErrLLMResourceConflict   = errors.New("LLM configuration resource conflict")
	ErrLLMResourceReferenced = errors.New("LLM configuration resource is referenced")
	ErrLLMWriteUnverified    = errors.New("agentgateway configuration write could not be verified")
)

const (
	changeCreateProvider = "create-llm-provider"
	changeUpdateProvider = "update-llm-provider"
	changeDeleteProvider = "delete-llm-provider"
	changeCreateModel    = "create-llm-model"
	changeUpdateModel    = "update-llm-model"
	changeDeleteModel    = "delete-llm-model"
)

var (
	environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	providerTypes   = map[string]struct{}{
		"openai": {}, "anthropic": {}, "gemini": {}, "vertex": {}, "bedrock": {},
		"azure": {}, "copilot": {}, "cohere": {}, "ollama": {}, "baseten": {},
		"cerebras": {}, "deepinfra": {}, "deepseek": {}, "groq": {}, "huggingface": {},
		"mistral": {}, "openrouter": {}, "togetherai": {}, "xai": {}, "fireworks": {},
		"custom": {},
	}
	providerFormats = map[string]struct{}{
		"completions": {}, "messages": {}, "responses": {}, "embeddings": {},
		"anthropicTokenCount": {}, "realtime": {}, "rerank": {},
	}
	managedParamKeys = []string{
		"model", "baseUrl", "awsRegion", "vertexRegion", "vertexProject",
		"azureResourceName", "azureResourceType", "azureApiVersion", "azureProjectName",
		"tokenize",
	}
)

type llmConfigDocument struct {
	root          map[string]json.RawMessage
	llm           map[string]json.RawMessage
	providers     []json.RawMessage
	models        []json.RawMessage
	virtualModels []json.RawMessage
}

type editableProvider struct {
	Name     string          `json:"name"`
	Provider json.RawMessage `json:"provider"`
	Params   json.RawMessage `json:"params"`
	Defaults json.RawMessage `json:"defaults"`
}

type editableModel struct {
	Name           string          `json:"name"`
	Provider       json.RawMessage `json:"provider"`
	Params         json.RawMessage `json:"params"`
	Visibility     string          `json:"visibility"`
	Transformation json.RawMessage `json:"transformation"`
	Auth           json.RawMessage `json:"auth"`
}

func (client *Client) LLMConfiguration(ctx context.Context) (model.LLMConfiguration, string, error) {
	payload, document, revision, err := client.readLLMDocument(ctx)
	if err != nil {
		return model.LLMConfiguration{}, "", err
	}
	_ = payload
	configuration, err := document.sanitizedConfiguration(time.Now().UTC())
	if err != nil {
		return model.LLMConfiguration{}, "", err
	}
	return configuration, revision, nil
}

func (client *Client) ApplyLLMChange(ctx context.Context, expectedRevision string, change model.LLMChange) (string, error) {
	_, document, revision, err := client.readLLMDocument(ctx)
	if err != nil {
		return "", err
	}
	if revision != expectedRevision {
		return "", ErrConfigurationChanged
	}
	targetName, err := document.changeTargetName(change)
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
	configuration, _, err := client.LLMConfiguration(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrLLMWriteUnverified, err)
	}
	if !changeVerified(configuration, change, targetName) {
		return "", ErrLLMWriteUnverified
	}
	return targetName, nil
}

func (document *llmConfigDocument) changeTargetName(change model.LLMChange) (string, error) {
	switch change.Operation {
	case changeCreateProvider, changeUpdateProvider:
		return change.Provider.Name, nil
	case changeCreateModel, changeUpdateModel:
		return change.Model.Name, nil
	case changeDeleteProvider:
		_, raw, err := document.providerByID(change.ResourceID)
		if err != nil {
			return "", err
		}
		var item editableProvider
		if json.Unmarshal(raw, &item) != nil || item.Name == "" {
			return "", ErrLLMInvalidRequest
		}
		return item.Name, nil
	case changeDeleteModel:
		_, raw, err := document.modelByID(change.ResourceID)
		if err != nil {
			return "", err
		}
		var item editableModel
		if json.Unmarshal(raw, &item) != nil || item.Name == "" {
			return "", ErrLLMInvalidRequest
		}
		return item.Name, nil
	default:
		return "", ErrLLMInvalidRequest
	}
}

func (client *Client) readLLMDocument(ctx context.Context) (json.RawMessage, *llmConfigDocument, string, error) {
	var payload json.RawMessage
	if _, err := client.upstream.GetJSON(ctx, "/api/config", &payload); err != nil {
		return nil, nil, "", err
	}
	document, err := decodeLLMDocument(payload)
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

func decodeLLMDocument(payload json.RawMessage) (*llmConfigDocument, error) {
	root := make(map[string]json.RawMessage)
	if err := json.Unmarshal(payload, &root); err != nil || root == nil {
		return nil, &ContractError{Field: "/api/config", Problem: "expected configuration object"}
	}
	llm := make(map[string]json.RawMessage)
	if raw := root["llm"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &llm); err != nil || llm == nil {
			return nil, &ContractError{Field: "/llm", Problem: "expected object"}
		}
	}
	document := &llmConfigDocument{root: root, llm: llm}
	if err := decodeArray(llm["providers"], "/llm/providers", &document.providers); err != nil {
		return nil, err
	}
	if err := decodeArray(llm["models"], "/llm/models", &document.models); err != nil {
		return nil, err
	}
	if err := decodeArray(llm["virtualModels"], "/llm/virtualModels", &document.virtualModels); err != nil {
		return nil, err
	}
	return document, nil
}

func decodeArray(raw json.RawMessage, field string, destination *[]json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		*destination = []json.RawMessage{}
		return nil
	}
	if err := json.Unmarshal(raw, destination); err != nil || *destination == nil {
		return &ContractError{Field: field, Problem: "expected array"}
	}
	return nil
}

func (document *llmConfigDocument) marshal() ([]byte, error) {
	providers, err := json.Marshal(document.providers)
	if err != nil {
		return nil, err
	}
	models, err := json.Marshal(document.models)
	if err != nil {
		return nil, err
	}
	virtualModels, err := json.Marshal(document.virtualModels)
	if err != nil {
		return nil, err
	}
	document.llm["providers"] = providers
	document.llm["models"] = models
	document.llm["virtualModels"] = virtualModels
	llm, err := json.Marshal(document.llm)
	if err != nil {
		return nil, err
	}
	document.root["llm"] = llm
	return json.Marshal(document.root)
}

func (document *llmConfigDocument) sanitizedConfiguration(fetchedAt time.Time) (model.LLMConfiguration, error) {
	configuration := model.LLMConfiguration{
		Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
		Providers: []model.LLMProviderSetting{}, Models: []model.LLMModelSetting{},
		VirtualModels: []model.GatewayModel{},
	}
	providerIndexes := make(map[string]int, len(document.providers))
	for index, raw := range document.providers {
		field := fmt.Sprintf("/llm/providers/%d", index)
		setting, err := providerSetting(raw, field, fetchedAt)
		if err != nil {
			return model.LLMConfiguration{}, err
		}
		providerIndexes[setting.Name] = len(configuration.Providers)
		configuration.Providers = append(configuration.Providers, setting)
	}
	for index, raw := range document.models {
		field := fmt.Sprintf("/llm/models/%d", index)
		setting, err := modelSetting(raw, field, fetchedAt)
		if err != nil {
			return model.LLMConfiguration{}, err
		}
		if setting.ProviderMode == "reference" {
			if providerIndex, ok := providerIndexes[setting.ProviderReference]; ok {
				configuration.Providers[providerIndex].ModelCount++
			}
		}
		configuration.Models = append(configuration.Models, setting)
	}
	for index, raw := range document.virtualModels {
		field := fmt.Sprintf("/llm/virtualModels/%d", index)
		var item rawVirtualModel
		if err := json.Unmarshal(raw, &item); err != nil || item.Name == "" {
			return model.LLMConfiguration{}, &ContractError{Field: field, Problem: "invalid virtual model"}
		}
		routing, targets, err := virtualRouting(item, field+"/routing")
		if err != nil {
			return model.LLMConfiguration{}, err
		}
		configuration.VirtualModels = append(configuration.VirtualModels, model.GatewayModel{
			ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, Kind: "virtual",
			Routing: routing, Targets: targets,
		})
	}
	return configuration, nil
}

func providerSetting(raw json.RawMessage, field string, fetchedAt time.Time) (model.LLMProviderSetting, error) {
	var item editableProvider
	if err := json.Unmarshal(raw, &item); err != nil || item.Name == "" {
		return model.LLMProviderSetting{}, &ContractError{Field: field, Problem: "invalid provider object"}
	}
	kind, reference, err := providerKind(item.Provider, field+"/provider")
	if err != nil {
		return model.LLMProviderSetting{}, err
	}
	if reference != "" {
		return model.LLMProviderSetting{}, &ContractError{Field: field + "/provider", Problem: "shared provider references are unverified"}
	}
	kind = canonicalProviderType(kind)
	if !knownProviderType(kind) {
		return model.LLMProviderSetting{}, &ContractError{Field: field + "/provider", Problem: "unsupported provider type"}
	}
	params, credential, err := sanitizedParams(item.Params, field+"/params")
	if err != nil {
		return model.LLMProviderSetting{}, err
	}
	formats, err := customFormats(item.Provider, field+"/provider")
	if err != nil {
		return model.LLMProviderSetting{}, err
	}
	if err := validateFormats(kind, formats); err != nil {
		return model.LLMProviderSetting{}, &ContractError{Field: field + "/provider/custom/formats", Problem: "unsupported custom provider formats"}
	}
	credential = combineCredentialStates(credential, authCredentialState(nestedRawField(item.Defaults, "auth")))
	return model.LLMProviderSetting{
		ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, ProviderType: kind,
		Params: params, Formats: formats, Credential: credential, Editable: true,
	}, nil
}

func modelSetting(raw json.RawMessage, field string, fetchedAt time.Time) (model.LLMModelSetting, error) {
	var item editableModel
	if err := json.Unmarshal(raw, &item); err != nil || item.Name == "" {
		return model.LLMModelSetting{}, &ContractError{Field: field, Problem: "invalid model object"}
	}
	kind, reference, err := providerKind(item.Provider, field+"/provider")
	if err != nil {
		return model.LLMModelSetting{}, err
	}
	mode := "builtin"
	providerType := canonicalProviderType(kind)
	if reference != "" {
		mode = "reference"
		providerType = ""
	} else if providerType == "custom" {
		mode = "custom"
	} else if !knownProviderType(providerType) {
		return model.LLMModelSetting{}, &ContractError{Field: field + "/provider", Problem: "unsupported provider type"}
	}
	params, credential, err := sanitizedParams(item.Params, field+"/params")
	if err != nil {
		return model.LLMModelSetting{}, err
	}
	credential = combineCredentialStates(credential, authCredentialState(item.Auth))
	formats, err := customFormats(item.Provider, field+"/provider")
	if err != nil {
		return model.LLMModelSetting{}, err
	}
	if err := validateFormats(providerType, formats); err != nil {
		return model.LLMModelSetting{}, &ContractError{Field: field + "/provider/custom/formats", Problem: "unsupported custom provider formats"}
	}
	visibility := item.Visibility
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "internal" {
		return model.LLMModelSetting{}, &ContractError{Field: field + "/visibility", Problem: "unsupported visibility"}
	}
	upstreamMode, modelExpression, err := modelUpstreamMode(item.Name, params.Model, item.Transformation, field+"/transformation")
	if err != nil {
		return model.LLMModelSetting{}, err
	}
	return model.LLMModelSetting{
		ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, ProviderMode: mode,
		ProviderType: providerType, ProviderReference: reference, Params: params, Formats: formats,
		Visibility: visibility, UpstreamMode: upstreamMode, ModelExpression: modelExpression,
		Credential: credential, Editable: true,
	}, nil
}

func sanitizedParams(raw json.RawMessage, field string) (model.LLMProviderParams, model.LLMCredentialState, error) {
	params := model.LLMProviderParams{}
	if len(raw) == 0 || string(raw) == "null" {
		return params, model.LLMCredentialState{Kind: "ambient"}, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, model.LLMCredentialState{}, &ContractError{Field: field, Problem: "invalid provider params"}
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return params, model.LLMCredentialState{}, &ContractError{Field: field, Problem: "expected object"}
	}
	return params, apiKeyCredentialState(values["apiKey"]), nil
}

func customFormats(raw json.RawMessage, field string) ([]model.LLMProviderFormat, error) {
	formats := []model.LLMProviderFormat{}
	var builtin string
	if json.Unmarshal(raw, &builtin) == nil && builtin != "" {
		return formats, nil
	}
	var value struct {
		Reference string `json:"reference"`
		Custom    *struct {
			Formats []model.LLMProviderFormat `json:"formats"`
		} `json:"custom"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, &ContractError{Field: field, Problem: "invalid provider"}
	}
	if value.Reference != "" || value.Custom == nil {
		return formats, nil
	}
	if value.Custom.Formats == nil {
		return nil, &ContractError{Field: field + "/custom/formats", Problem: "required array is missing"}
	}
	return value.Custom.Formats, nil
}

func nestedRawField(raw json.RawMessage, name string) json.RawMessage {
	if !rawConfigured(raw) {
		return nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values[name]
}

func rawConfigured(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null" && string(raw) != `""` && string(raw) != "{}"
}

func apiKeyCredentialState(raw json.RawMessage) model.LLMCredentialState {
	if !rawConfigured(raw) {
		return model.LLMCredentialState{Kind: "ambient"}
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		kind := "literal"
		if strings.HasPrefix(value, "$") {
			kind = "environment"
		}
		return model.LLMCredentialState{Configured: true, Kind: kind}
	}
	var file struct {
		File string `json:"file"`
	}
	if json.Unmarshal(raw, &file) == nil && file.File != "" {
		return model.LLMCredentialState{Configured: true, Kind: "file"}
	}
	return model.LLMCredentialState{Configured: true, Kind: "other"}
}

func authCredentialState(raw json.RawMessage) model.LLMCredentialState {
	if !rawConfigured(raw) {
		return model.LLMCredentialState{Kind: "ambient"}
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return model.LLMCredentialState{Configured: true, Kind: "other"}
	}
	if aws, ok := value["aws"].(map[string]any); ok {
		if _, hasAccessKey := aws["accessKeyId"].(string); hasAccessKey {
			return model.LLMCredentialState{Configured: true, Kind: "aws-static"}
		}
	}
	if gcp, ok := value["gcp"].(map[string]any); ok {
		if credential, ok := gcp["credential"].(map[string]any); ok {
			if _, hasFile := credential["file"].(string); hasFile {
				return model.LLMCredentialState{Configured: true, Kind: "gcp-file"}
			}
		}
	}
	if azure, ok := value["azure"].(map[string]any); ok {
		if explicit, ok := azure["explicitConfig"].(map[string]any); ok {
			if _, managed := explicit["managedIdentity"].(map[string]any); managed {
				return model.LLMCredentialState{Configured: true, Kind: "azure-managed-identity"}
			}
		}
	}
	return model.LLMCredentialState{Configured: true, Kind: "other"}
}

func combineCredentialStates(left, right model.LLMCredentialState) model.LLMCredentialState {
	if !left.Configured {
		return right
	}
	if !right.Configured {
		return left
	}
	return model.LLMCredentialState{Configured: true, Kind: "other"}
}

func modelUpstreamMode(name, explicitModel string, transformation json.RawMessage, field string) (string, string, error) {
	if explicitModel != "" {
		return "explicit", "", nil
	}
	if !rawConfigured(transformation) {
		return "incoming", "", nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(transformation, &values) != nil || values == nil {
		return "", "", &ContractError{Field: field, Problem: "expected object"}
	}
	rawExpression := values["model"]
	if !rawConfigured(rawExpression) {
		return "incoming", "", nil
	}
	var expression string
	if json.Unmarshal(rawExpression, &expression) != nil || expression == "" {
		return "", "", &ContractError{Field: field + "/model", Problem: "expected CEL expression string"}
	}
	if expression == stripPrefixExpression(name) {
		return "strip", "", nil
	}
	return "custom", expression, nil
}

func stripPrefixExpression(name string) string {
	index := strings.Index(name, "/")
	if index < 0 {
		return ""
	}
	prefix := name[:index+1]
	return fmt.Sprintf(`llmRequest.model.stripPrefix(%q)`, prefix)
}

func (document *llmConfigDocument) apply(change model.LLMChange) error {
	switch change.Operation {
	case changeCreateProvider:
		return document.createProvider(change.Provider)
	case changeUpdateProvider:
		return document.updateProvider(change.ResourceID, change.Provider)
	case changeDeleteProvider:
		return document.deleteProvider(change.ResourceID)
	case changeCreateModel:
		return document.createModel(change.Model)
	case changeUpdateModel:
		return document.updateModel(change.ResourceID, change.Model)
	case changeDeleteModel:
		return document.deleteModel(change.ResourceID)
	default:
		return ErrLLMInvalidRequest
	}
}

func (document *llmConfigDocument) createProvider(draft model.LLMProviderDraft) error {
	if err := validateProviderDraft(draft, false); err != nil {
		return err
	}
	if document.providerNameExists(draft.Name, -1) {
		return ErrLLMResourceConflict
	}
	entry, err := buildProviderEntry(nil, draft, false)
	if err != nil {
		return err
	}
	document.providers = append(document.providers, entry)
	return nil
}

func (document *llmConfigDocument) updateProvider(id string, draft model.LLMProviderDraft) error {
	if err := validateProviderDraft(draft, true); err != nil {
		return err
	}
	index, existing, err := document.providerByID(id)
	if err != nil {
		return err
	}
	var current editableProvider
	if json.Unmarshal(existing, &current) != nil {
		return ErrLLMInvalidRequest
	}
	currentType, _, err := providerKind(current.Provider, fmt.Sprintf("/llm/providers/%d/provider", index))
	if err != nil {
		return err
	}
	if canonicalProviderType(currentType) != draft.ProviderType {
		return ErrLLMInvalidRequest
	}
	if document.providerNameExists(draft.Name, index) {
		return ErrLLMResourceConflict
	}
	if current.Name != draft.Name && document.providerReferenced(current.Name) {
		return ErrLLMResourceReferenced
	}
	entry, err := buildProviderEntry(existing, draft, true)
	if err != nil {
		return err
	}
	document.providers[index] = entry
	return nil
}

func (document *llmConfigDocument) deleteProvider(id string) error {
	index, existing, err := document.providerByID(id)
	if err != nil {
		return err
	}
	var current editableProvider
	if json.Unmarshal(existing, &current) != nil {
		return ErrLLMInvalidRequest
	}
	if document.providerReferenced(current.Name) {
		return ErrLLMResourceReferenced
	}
	document.providers = append(document.providers[:index], document.providers[index+1:]...)
	return nil
}

func (document *llmConfigDocument) createModel(draft model.LLMModelDraft) error {
	if err := document.validateModelDraft(draft, false); err != nil {
		return err
	}
	if document.modelNameExists(draft.Name, -1) {
		return ErrLLMResourceConflict
	}
	entry, err := buildModelEntry(nil, draft, false)
	if err != nil {
		return err
	}
	document.models = append(document.models, entry)
	return nil
}

func (document *llmConfigDocument) updateModel(id string, draft model.LLMModelDraft) error {
	if err := document.validateModelDraft(draft, true); err != nil {
		return err
	}
	index, existing, err := document.modelByID(id)
	if err != nil {
		return err
	}
	current, err := modelSetting(existing, fmt.Sprintf("/llm/models/%d", index), time.Now().UTC())
	if err != nil {
		return err
	}
	if current.ProviderMode != draft.ProviderMode || current.ProviderType != draft.ProviderType || current.ProviderReference != draft.ProviderReference {
		return ErrLLMInvalidRequest
	}
	if document.modelNameExists(draft.Name, index) {
		return ErrLLMResourceConflict
	}
	if current.Name != draft.Name && document.modelReferenced(current.Name) {
		return ErrLLMResourceReferenced
	}
	entry, err := buildModelEntry(existing, draft, true)
	if err != nil {
		return err
	}
	document.models[index] = entry
	return nil
}

func (document *llmConfigDocument) deleteModel(id string) error {
	index, existing, err := document.modelByID(id)
	if err != nil {
		return err
	}
	var current editableModel
	if json.Unmarshal(existing, &current) != nil {
		return ErrLLMInvalidRequest
	}
	if document.modelReferenced(current.Name) {
		return ErrLLMResourceReferenced
	}
	document.models = append(document.models[:index], document.models[index+1:]...)
	return nil
}

func buildProviderEntry(existing json.RawMessage, draft model.LLMProviderDraft, updating bool) (json.RawMessage, error) {
	entry := make(map[string]json.RawMessage)
	if len(existing) > 0 && json.Unmarshal(existing, &entry) != nil {
		return nil, ErrLLMInvalidRequest
	}
	entry["name"] = mustJSON(draft.Name)
	provider, err := providerValue(entry["provider"], draft.ProviderType, draft.Formats)
	if err != nil {
		return nil, err
	}
	entry["provider"] = provider
	params, err := mergeParams(entry["params"], draft.Params, draft.Credential, updating)
	if err != nil {
		return nil, err
	}
	if len(params) == 0 {
		delete(entry, "params")
	} else {
		entry["params"] = params
	}
	if err := applyProviderCredential(entry, draft.Credential); err != nil {
		return nil, err
	}
	return json.Marshal(entry)
}

func buildModelEntry(existing json.RawMessage, draft model.LLMModelDraft, updating bool) (json.RawMessage, error) {
	entry := make(map[string]json.RawMessage)
	if len(existing) > 0 && json.Unmarshal(existing, &entry) != nil {
		return nil, ErrLLMInvalidRequest
	}
	entry["name"] = mustJSON(draft.Name)
	switch draft.ProviderMode {
	case "reference":
		entry["provider"] = mustJSON(map[string]string{"reference": draft.ProviderReference})
	case "custom":
		provider, err := providerValue(entry["provider"], "custom", draft.Formats)
		if err != nil {
			return nil, err
		}
		entry["provider"] = provider
	case "builtin":
		entry["provider"] = mustJSON(draft.ProviderType)
	default:
		return nil, ErrLLMInvalidRequest
	}
	params, err := mergeParams(entry["params"], draft.Params, draft.Credential, updating)
	if err != nil {
		return nil, err
	}
	if len(params) == 0 {
		delete(entry, "params")
	} else {
		entry["params"] = params
	}
	transformation, err := mergeModelTransformation(entry["transformation"], draft)
	if err != nil {
		return nil, err
	}
	if len(transformation) == 0 {
		delete(entry, "transformation")
	} else {
		entry["transformation"] = transformation
	}
	if err := applyModelCredential(entry, draft.Credential); err != nil {
		return nil, err
	}
	entry["visibility"] = mustJSON(draft.Visibility)
	return json.Marshal(entry)
}

func removeNestedField(entry map[string]json.RawMessage, objectName, fieldName string) error {
	raw := entry[objectName]
	if !rawConfigured(raw) {
		return nil
	}
	values := make(map[string]json.RawMessage)
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return ErrLLMInvalidRequest
	}
	delete(values, fieldName)
	if len(values) == 0 {
		delete(entry, objectName)
		return nil
	}
	entry[objectName] = rawObject(values)
	return nil
}

func setNestedField(entry map[string]json.RawMessage, objectName, fieldName string, value json.RawMessage) error {
	values := make(map[string]json.RawMessage)
	if raw := entry[objectName]; rawConfigured(raw) {
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return ErrLLMInvalidRequest
		}
	}
	values[fieldName] = value
	entry[objectName] = rawObject(values)
	return nil
}

func applyProviderCredential(entry map[string]json.RawMessage, credential model.LLMCredentialInput) error {
	if credential.Mode == "preserve" {
		return nil
	}
	auth := credentialAuth(credential)
	if len(auth) == 0 {
		return removeNestedField(entry, "defaults", "auth")
	}
	return setNestedField(entry, "defaults", "auth", auth)
}

func applyModelCredential(entry map[string]json.RawMessage, credential model.LLMCredentialInput) error {
	if credential.Mode == "preserve" {
		return nil
	}
	auth := credentialAuth(credential)
	if len(auth) == 0 {
		delete(entry, "auth")
	} else {
		entry["auth"] = auth
	}
	return nil
}

func credentialAuth(credential model.LLMCredentialInput) json.RawMessage {
	switch credential.Mode {
	case "aws-static":
		aws := map[string]any{
			"accessKeyId": credential.AccessKeyID, "secretAccessKey": credential.SecretAccessKey,
			"region": nil, "serviceName": nil, "sessionToken": nil,
		}
		if credential.SessionToken != "" {
			aws["sessionToken"] = credential.SessionToken
		}
		return mustJSON(map[string]any{"aws": aws})
	case "gcp-file":
		return mustJSON(map[string]any{"gcp": map[string]any{"credential": map[string]string{"file": credential.Reference}}})
	case "azure-managed-identity":
		var identity any
		if credential.ClientID != "" {
			identity = map[string]string{"clientId": strings.TrimSpace(credential.ClientID)}
		}
		return mustJSON(map[string]any{"azure": map[string]any{
			"explicitConfig": map[string]any{
				"managedIdentity": map[string]any{"userAssignedIdentity": identity},
			},
		}})
	default:
		return nil
	}
}

func mergeModelTransformation(existing json.RawMessage, draft model.LLMModelDraft) (json.RawMessage, error) {
	transformation := make(map[string]json.RawMessage)
	if rawConfigured(existing) && json.Unmarshal(existing, &transformation) != nil {
		return nil, ErrLLMInvalidRequest
	}
	delete(transformation, "model")
	switch draft.UpstreamMode {
	case "incoming", "explicit":
	case "strip":
		transformation["model"] = mustJSON(stripPrefixExpression(draft.Name))
	case "custom":
		transformation["model"] = mustJSON(draft.ModelExpression)
	default:
		return nil, ErrLLMInvalidRequest
	}
	if len(transformation) == 0 {
		return nil, nil
	}
	return json.Marshal(transformation)
}

func providerValue(existing json.RawMessage, providerType string, formats []model.LLMProviderFormat) (json.RawMessage, error) {
	if providerType != "custom" {
		return json.Marshal(providerType)
	}
	custom := make(map[string]json.RawMessage)
	var wrapper struct {
		Custom json.RawMessage `json:"custom"`
	}
	if json.Unmarshal(existing, &wrapper) == nil && len(wrapper.Custom) > 0 {
		_ = json.Unmarshal(wrapper.Custom, &custom)
	}
	custom["formats"] = mustJSON(formats)
	return json.Marshal(map[string]any{"custom": rawObject(custom)})
}

func rawObject(values map[string]json.RawMessage) json.RawMessage {
	encoded, _ := json.Marshal(values)
	return encoded
}

func mergeParams(existing json.RawMessage, draft model.LLMProviderParams, credential model.LLMCredentialInput, updating bool) (json.RawMessage, error) {
	params := make(map[string]json.RawMessage)
	if rawConfigured(existing) && json.Unmarshal(existing, &params) != nil {
		return nil, ErrLLMInvalidRequest
	}
	for _, key := range managedParamKeys {
		delete(params, key)
	}
	setStringParam(params, "model", draft.Model)
	setStringParam(params, "baseUrl", draft.BaseURL)
	setStringParam(params, "awsRegion", draft.AWSRegion)
	setStringParam(params, "vertexRegion", draft.VertexRegion)
	setStringParam(params, "vertexProject", draft.VertexProject)
	setStringParam(params, "azureResourceName", draft.AzureResourceName)
	setStringParam(params, "azureResourceType", draft.AzureResourceType)
	setStringParam(params, "azureApiVersion", draft.AzureAPIVersion)
	setStringParam(params, "azureProjectName", draft.AzureProjectName)
	if draft.Tokenize {
		params["tokenize"] = mustJSON(true)
	}
	switch credential.Mode {
	case "preserve":
		if !updating {
			return nil, ErrLLMInvalidRequest
		}
	case "ambient", "aws-static", "gcp-file", "azure-managed-identity":
		delete(params, "apiKey")
	case "environment":
		if !environmentName.MatchString(credential.Reference) {
			return nil, ErrLLMInvalidRequest
		}
		params["apiKey"] = mustJSON("$" + credential.Reference)
	case "file":
		path := strings.TrimSpace(credential.Reference)
		if path == "" || len(path) > 1024 || strings.ContainsAny(path, "\r\n") {
			return nil, ErrLLMInvalidRequest
		}
		params["apiKey"] = mustJSON(map[string]string{"file": path})
	case "literal":
		params["apiKey"] = mustJSON(strings.TrimSpace(credential.Secret))
	default:
		return nil, ErrLLMInvalidRequest
	}
	if len(params) == 0 {
		return nil, nil
	}
	return json.Marshal(params)
}

func setStringParam(values map[string]json.RawMessage, key, value string) {
	if value != "" {
		values[key] = mustJSON(value)
	}
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func validateProviderDraft(draft model.LLMProviderDraft, updating bool) error {
	if !validConfigName(draft.Name) || !knownProviderType(draft.ProviderType) {
		return ErrLLMInvalidRequest
	}
	if err := validateParams(draft.Params); err != nil {
		return err
	}
	if err := validateProviderParams(draft.ProviderType, draft.Params); err != nil {
		return err
	}
	if err := validateFormats(draft.ProviderType, draft.Formats); err != nil {
		return err
	}
	if draft.ProviderType == "custom" && draft.Params.BaseURL == "" {
		return ErrLLMInvalidRequest
	}
	return validateProviderCredential(draft.ProviderType, draft.Credential, updating)
}

func (document *llmConfigDocument) validateModelDraft(draft model.LLMModelDraft, updating bool) error {
	if !validConfigName(draft.Name) || (draft.Visibility != "public" && draft.Visibility != "internal") {
		return ErrLLMInvalidRequest
	}
	if err := validateParams(draft.Params); err != nil {
		return err
	}
	if err := validateModelUpstream(draft); err != nil {
		return err
	}
	switch draft.ProviderMode {
	case "reference":
		if !validConfigName(draft.ProviderReference) || !document.providerNameExists(draft.ProviderReference, -1) {
			return ErrLLMInvalidRequest
		}
		if draft.ProviderType != "" || len(draft.Formats) > 0 || !onlyModelParam(draft.Params) {
			return ErrLLMInvalidRequest
		}
	case "builtin":
		if !knownProviderType(draft.ProviderType) || draft.ProviderType == "custom" || len(draft.Formats) > 0 {
			return ErrLLMInvalidRequest
		}
		if err := validateProviderParams(draft.ProviderType, draft.Params); err != nil {
			return err
		}
	case "custom":
		if draft.ProviderType != "custom" || draft.Params.BaseURL == "" {
			return ErrLLMInvalidRequest
		}
		if err := validateFormats("custom", draft.Formats); err != nil {
			return err
		}
		if err := validateProviderParams("custom", draft.Params); err != nil {
			return err
		}
	default:
		return ErrLLMInvalidRequest
	}
	return validateProviderCredential(draft.ProviderType, draft.Credential, updating)
}

func validateProviderCredential(providerType string, credential model.LLMCredentialInput, updating bool) error {
	if err := validateCredential(credential, updating); err != nil {
		return err
	}
	allowed := map[string]struct{}{"preserve": {}, "ambient": {}}
	switch providerType {
	case "":
	case "bedrock":
		allowed["aws-static"] = struct{}{}
	case "vertex":
		allowed["gcp-file"] = struct{}{}
	case "azure":
		allowed["environment"] = struct{}{}
		allowed["literal"] = struct{}{}
		allowed["file"] = struct{}{}
		allowed["azure-managed-identity"] = struct{}{}
	default:
		allowed["environment"] = struct{}{}
		allowed["literal"] = struct{}{}
		allowed["file"] = struct{}{}
	}
	if _, ok := allowed[credential.Mode]; !ok {
		return ErrLLMInvalidRequest
	}
	return nil
}

func validateCredential(credential model.LLMCredentialInput, updating bool) error {
	switch credential.Mode {
	case "preserve":
		if !updating || !credentialFieldsEmpty(credential) {
			return ErrLLMInvalidRequest
		}
	case "ambient":
		if !credentialFieldsEmpty(credential) {
			return ErrLLMInvalidRequest
		}
	case "environment":
		if !environmentName.MatchString(credential.Reference) || !credentialFieldsEmptyExcept(credential, "reference") {
			return ErrLLMInvalidRequest
		}
	case "file", "gcp-file":
		if !validCredentialValue(credential.Reference, 1024) || strings.TrimSpace(credential.Reference) != credential.Reference ||
			!credentialFieldsEmptyExcept(credential, "reference") {
			return ErrLLMInvalidRequest
		}
	case "literal":
		if !validCredentialValue(credential.Secret, 8192) || strings.TrimSpace(credential.Secret) != credential.Secret ||
			!credentialFieldsEmptyExcept(credential, "secret") {
			return ErrLLMInvalidRequest
		}
	case "aws-static":
		if !validCredentialValue(credential.AccessKeyID, 512) ||
			!validCredentialValue(credential.SecretAccessKey, 8192) ||
			(credential.SessionToken != "" && !validCredentialValue(credential.SessionToken, 8192)) ||
			!credentialFieldsEmptyExcept(credential, "accessKeyId", "secretAccessKey", "sessionToken") {
			return ErrLLMInvalidRequest
		}
	case "azure-managed-identity":
		if (credential.ClientID != "" && (!validCredentialValue(credential.ClientID, 512) || strings.TrimSpace(credential.ClientID) != credential.ClientID)) ||
			!credentialFieldsEmptyExcept(credential, "clientId") {
			return ErrLLMInvalidRequest
		}
	default:
		return ErrLLMInvalidRequest
	}
	return nil
}

func validCredentialValue(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}

func credentialFieldsEmpty(credential model.LLMCredentialInput) bool {
	return credentialFieldsEmptyExcept(credential)
}

func credentialFieldsEmptyExcept(credential model.LLMCredentialInput, fields ...string) bool {
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
	}
	values := map[string]string{
		"reference": credential.Reference, "secret": credential.Secret,
		"accessKeyId": credential.AccessKeyID, "secretAccessKey": credential.SecretAccessKey,
		"sessionToken": credential.SessionToken, "clientId": credential.ClientID,
	}
	for field, value := range values {
		if _, ok := allowed[field]; !ok && value != "" {
			return false
		}
	}
	return true
}

func validateModelUpstream(draft model.LLMModelDraft) error {
	switch draft.UpstreamMode {
	case "incoming":
		if draft.Params.Model != "" || draft.ModelExpression != "" {
			return ErrLLMInvalidRequest
		}
	case "explicit":
		if draft.Params.Model == "" || draft.ModelExpression != "" {
			return ErrLLMInvalidRequest
		}
	case "strip":
		if draft.Params.Model != "" || draft.ModelExpression != "" || stripPrefixExpression(draft.Name) == "" {
			return ErrLLMInvalidRequest
		}
	case "custom":
		if draft.Params.Model != "" || strings.TrimSpace(draft.ModelExpression) != draft.ModelExpression ||
			draft.ModelExpression == "" || len(draft.ModelExpression) > 4096 || strings.ContainsRune(draft.ModelExpression, '\x00') {
			return ErrLLMInvalidRequest
		}
	default:
		return ErrLLMInvalidRequest
	}
	return nil
}

func validateParams(params model.LLMProviderParams) error {
	values := []struct {
		value string
		max   int
	}{
		{params.Model, 256}, {params.BaseURL, 2048}, {params.AWSRegion, 128},
		{params.VertexRegion, 128}, {params.VertexProject, 256}, {params.AzureResourceName, 256},
		{params.AzureAPIVersion, 128}, {params.AzureProjectName, 256},
	}
	for _, value := range values {
		if len(value.value) > value.max || strings.ContainsAny(value.value, "\r\n") {
			return ErrLLMInvalidRequest
		}
	}
	if params.AzureResourceType != "" && params.AzureResourceType != "openAI" && params.AzureResourceType != "foundry" {
		return ErrLLMInvalidRequest
	}
	if params.BaseURL != "" {
		parsed, err := url.Parse(params.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return ErrLLMInvalidRequest
		}
	}
	return nil
}

func validateProviderParams(providerType string, params model.LLMProviderParams) error {
	if params.AWSRegion != "" && providerType != "bedrock" {
		return ErrLLMInvalidRequest
	}
	if (params.VertexRegion != "" || params.VertexProject != "") && providerType != "vertex" {
		return ErrLLMInvalidRequest
	}
	if (params.AzureResourceName != "" || params.AzureResourceType != "" || params.AzureAPIVersion != "" || params.AzureProjectName != "") && providerType != "azure" {
		return ErrLLMInvalidRequest
	}
	return nil
}

func onlyModelParam(params model.LLMProviderParams) bool {
	return params.BaseURL == "" && params.AWSRegion == "" && params.VertexRegion == "" &&
		params.VertexProject == "" && params.AzureResourceName == "" &&
		params.AzureResourceType == "" && params.AzureAPIVersion == "" && params.AzureProjectName == "" && !params.Tokenize
}

func validateFormats(providerType string, formats []model.LLMProviderFormat) error {
	if providerType != "custom" {
		if len(formats) > 0 {
			return ErrLLMInvalidRequest
		}
		return nil
	}
	if len(formats) == 0 || len(formats) > len(providerFormats) {
		return ErrLLMInvalidRequest
	}
	seen := make(map[string]struct{}, len(formats))
	for _, format := range formats {
		if _, ok := providerFormats[format.Type]; !ok {
			return ErrLLMInvalidRequest
		}
		if _, duplicate := seen[format.Type]; duplicate {
			return ErrLLMInvalidRequest
		}
		seen[format.Type] = struct{}{}
		if len(format.Path) > 512 || strings.ContainsAny(format.Path, "\r\n") || (format.Path != "" && !strings.HasPrefix(format.Path, "/")) {
			return ErrLLMInvalidRequest
		}
	}
	return nil
}

func validConfigName(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= 256 && !strings.ContainsAny(value, "\r\n\t")
}

func knownProviderType(value string) bool {
	_, ok := providerTypes[value]
	return ok
}

func canonicalProviderType(value string) string {
	if value == "openAI" {
		return "openai"
	}
	return value
}

func (document *llmConfigDocument) providerByID(id string) (int, json.RawMessage, error) {
	for index, raw := range document.providers {
		field := fmt.Sprintf("/llm/providers/%d", index)
		if resource(field, "", time.Time{}).ID == id {
			return index, raw, nil
		}
	}
	return -1, nil, ErrLLMResourceNotFound
}

func (document *llmConfigDocument) modelByID(id string) (int, json.RawMessage, error) {
	for index, raw := range document.models {
		field := fmt.Sprintf("/llm/models/%d", index)
		if resource(field, "", time.Time{}).ID == id {
			return index, raw, nil
		}
	}
	return -1, nil, ErrLLMResourceNotFound
}

func (document *llmConfigDocument) providerNameExists(name string, except int) bool {
	for index, raw := range document.providers {
		if index == except {
			continue
		}
		var item editableProvider
		if json.Unmarshal(raw, &item) == nil && item.Name == name {
			return true
		}
	}
	return false
}

func (document *llmConfigDocument) modelNameExists(name string, except int) bool {
	for index, raw := range document.models {
		if index == except {
			continue
		}
		var item editableModel
		if json.Unmarshal(raw, &item) == nil && item.Name == name {
			return true
		}
	}
	return false
}

func (document *llmConfigDocument) providerReferenced(name string) bool {
	for index, raw := range document.models {
		var item editableModel
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		_, reference, err := providerKind(item.Provider, fmt.Sprintf("/llm/models/%d/provider", index))
		if err == nil && reference == name {
			return true
		}
	}
	return false
}

func (document *llmConfigDocument) modelReferenced(name string) bool {
	for _, raw := range document.virtualModels {
		var item rawVirtualModel
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		for _, targets := range [][]struct {
			Model string `json:"model"`
		}{weightedTargets(item), failoverTargets(item), conditionalTargets(item)} {
			for _, target := range targets {
				if target.Model == name {
					return true
				}
			}
		}
	}
	return false
}

func weightedTargets(item rawVirtualModel) []struct {
	Model string `json:"model"`
} {
	if item.Routing.Weighted == nil {
		return nil
	}
	return item.Routing.Weighted.Targets
}

func failoverTargets(item rawVirtualModel) []struct {
	Model string `json:"model"`
} {
	if item.Routing.Failover == nil {
		return nil
	}
	return item.Routing.Failover.Targets
}

func conditionalTargets(item rawVirtualModel) []struct {
	Model string `json:"model"`
} {
	if item.Routing.Conditional == nil {
		return nil
	}
	return item.Routing.Conditional.Targets
}

func changeVerified(configuration model.LLMConfiguration, change model.LLMChange, targetName string) bool {
	switch change.Operation {
	case changeCreateProvider, changeUpdateProvider:
		for _, item := range configuration.Providers {
			if item.Name == change.Provider.Name {
				return providerDraftMatches(item, change.Provider)
			}
		}
		return false
	case changeDeleteProvider:
		return !containsProvider(configuration.Providers, targetName)
	case changeCreateModel, changeUpdateModel:
		for _, item := range configuration.Models {
			if item.Name == change.Model.Name {
				return modelDraftMatches(item, change.Model)
			}
		}
		return false
	case changeDeleteModel:
		return !containsModel(configuration.Models, targetName)
	default:
		return false
	}
}

func providerDraftMatches(item model.LLMProviderSetting, draft model.LLMProviderDraft) bool {
	return item.ProviderType == draft.ProviderType && item.Params == draft.Params &&
		formatsMatch(item.Formats, draft.Formats) && credentialStateMatches(item.Credential, draft.Credential)
}

func modelDraftMatches(item model.LLMModelSetting, draft model.LLMModelDraft) bool {
	return item.ProviderMode == draft.ProviderMode && item.ProviderType == draft.ProviderType &&
		item.ProviderReference == draft.ProviderReference && item.Params == draft.Params &&
		formatsMatch(item.Formats, draft.Formats) && item.Visibility == draft.Visibility &&
		item.UpstreamMode == draft.UpstreamMode && item.ModelExpression == draft.ModelExpression &&
		credentialStateMatches(item.Credential, draft.Credential)
}

func formatsMatch(actual, expected []model.LLMProviderFormat) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func credentialStateMatches(state model.LLMCredentialState, input model.LLMCredentialInput) bool {
	switch input.Mode {
	case "preserve":
		return true
	case "ambient":
		return !state.Configured && state.Kind == "ambient"
	case "environment", "literal", "file", "aws-static", "gcp-file", "azure-managed-identity":
		return state.Configured && state.Kind == input.Mode
	default:
		return false
	}
}

func containsProvider(items []model.LLMProviderSetting, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func containsModel(items []model.LLMModelSetting, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}
