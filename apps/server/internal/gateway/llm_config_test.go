package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const editableConfig = `{
  "config":{"database":{"url":"sqlite://request-logs.db"}},
  "llm":{
    "providers":[{
      "name":"shared-custom",
      "provider":{"custom":{"formats":[{"type":"completions"}],"providerOverride":"deepseek"}},
      "params":{"baseUrl":"https://old.example/v1","apiKey":"never-return-existing-key","tokenize":true},
      "defaults":{"transformation":{"temperature":"0.2"}}
    }],
    "models":[
      {"name":"fast","provider":{"reference":"shared-custom"},"params":{"model":"upstream-fast"}},
      {"name":"openai/*","provider":"openai","params":{"apiKey":"never-return-model-key"}}
    ],
    "virtualModels":[{"name":"resilient","routing":{"failover":{"targets":[{"model":"fast","priority":0}]}}}],
    "policies":{"cors":{"allowOrigins":["https://console.example"]}}
  },
  "mcp":{"targets":[{"name":"tools","mcp":{"host":"localhost","port":3000}}]}
}`

type mutableConfigServer struct {
	mu         sync.Mutex
	config     json.RawMessage
	postCount  int
	lastPost   json.RawMessage
	ignorePost bool
}

func (server *mutableConfigServer) handler(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/api/config" {
		http.NotFound(writer, request)
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	writer.Header().Set("Content-Type", "application/json")
	if request.Method == http.MethodGet {
		_, _ = writer.Write(server.config)
		return
	}
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || !json.Valid(body) {
		http.Error(writer, "invalid", http.StatusBadRequest)
		return
	}
	server.postCount++
	server.lastPost = append(server.lastPost[:0], body...)
	if !server.ignorePost {
		server.config = append(server.config[:0], body...)
	}
	_, _ = writer.Write([]byte(`{"status":"success","message":"written"}`))
}

func TestLLMConfigurationReturnsEditableFieldsWithoutCredentials(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if revision == "" || len(configuration.Providers) != 1 || len(configuration.Models) != 2 || len(configuration.VirtualModels) != 1 {
		t.Fatalf("unexpected configuration: %#v revision=%q", configuration, revision)
	}
	provider := configuration.Providers[0]
	if provider.ProviderType != "custom" || provider.Params.BaseURL != "https://old.example/v1" || len(provider.Formats) != 1 || !provider.Credential.Configured || provider.ModelCount != 1 {
		t.Fatalf("unexpected provider projection: %#v", provider)
	}
	if configuration.Models[0].ProviderMode != "reference" || configuration.Models[0].ProviderReference != "shared-custom" || !configuration.Models[1].Credential.Configured {
		t.Fatalf("unexpected model projection: %#v", configuration.Models)
	}
	encoded, _ := json.Marshal(configuration)
	for _, forbidden := range []string{"never-return-existing-key", "never-return-model-key", "apiKey", "$EXISTING"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("configuration leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestApplyLLMChangePreservesSecretsAndUnrelatedConfiguration(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	provider := configuration.Providers[0]
	target, err := client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "update-llm-provider", ResourceID: provider.ID,
		Provider: model.LLMProviderDraft{
			Name: "shared-custom", ProviderType: "custom",
			Params:     model.LLMProviderParams{BaseURL: "https://new.example/v1", Tokenize: true},
			Formats:    []model.LLMProviderFormat{{Type: "completions"}, {Type: "responses", Path: "/v1/responses"}},
			Credential: model.LLMCredentialInput{Mode: "preserve"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "shared-custom" {
		t.Fatalf("target = %q", target)
	}
	state.mu.Lock()
	posted := append([]byte(nil), state.lastPost...)
	postCount := state.postCount
	state.mu.Unlock()
	if postCount != 1 {
		t.Fatalf("POST count = %d", postCount)
	}
	for _, preserved := range []string{
		"never-return-existing-key", "never-return-model-key", "sqlite://request-logs.db",
		"temperature", "console.example", `"mcp"`, `"tokenize":true`, "providerOverride",
	} {
		if !strings.Contains(string(posted), preserved) {
			t.Fatalf("write did not preserve %q: %s", preserved, posted)
		}
	}
	if !strings.Contains(string(posted), "https://new.example/v1") || !strings.Contains(string(posted), "/v1/responses") {
		t.Fatalf("write did not apply safe fields: %s", posted)
	}
}

func TestApplyLLMChangeRejectsSuccessfulResponseWhenFieldsWereNotApplied(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableConfig), ignorePost: true}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "update-llm-provider", ResourceID: configuration.Providers[0].ID,
		Provider: model.LLMProviderDraft{
			Name: "shared-custom", ProviderType: "custom",
			Params:     model.LLMProviderParams{BaseURL: "https://not-applied.example/v1"},
			Formats:    []model.LLMProviderFormat{{Type: "completions"}},
			Credential: model.LLMCredentialInput{Mode: "preserve"},
		},
	})
	if !errors.Is(err, ErrLLMWriteUnverified) {
		t.Fatalf("unverified write error = %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.postCount != 1 {
		t.Fatalf("POST count = %d", state.postCount)
	}
}

func TestApplyLLMChangeRejectsStaleRevisionAndReferencesWithoutWriting(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	configuration, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ApplyLLMChange(t.Context(), "stale", model.LLMChange{
		Operation: "delete-llm-provider", ResourceID: configuration.Providers[0].ID,
	})
	if !errors.Is(err, ErrConfigurationChanged) {
		t.Fatalf("stale error = %v", err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "delete-llm-provider", ResourceID: configuration.Providers[0].ID,
	})
	if !errors.Is(err, ErrLLMResourceReferenced) {
		t.Fatalf("reference error = %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.postCount != 0 {
		t.Fatalf("POST count = %d", state.postCount)
	}
}

func TestCreateProviderWritesEnvironmentReferenceOnly(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(`{"llm":{"providers":[],"models":[],"virtualModels":[]}}`)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "create-llm-provider",
		Provider: model.LLMProviderDraft{
			Name: "shared-openai", ProviderType: "openai", Formats: []model.LLMProviderFormat{},
			Credential: model.LLMCredentialInput{Mode: "environment", Reference: "OPENAI_API_KEY"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !strings.Contains(string(state.lastPost), `"apiKey":"$OPENAI_API_KEY"`) {
		t.Fatalf("environment reference missing: %s", state.lastPost)
	}
}

func TestCredentialModesWriteVerifiedShapesWithoutReturningValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		providerType string
		params       model.LLMProviderParams
		credential   model.LLMCredentialInput
		kind         string
		expected     []string
		forbidden    []string
	}{
		{
			name: "literal API key", providerType: "openai",
			credential: model.LLMCredentialInput{Mode: "literal", Secret: "write-only-literal-value"},
			kind:       "literal", expected: []string{`"apiKey":"write-only-literal-value"`},
			forbidden: []string{"write-only-literal-value"},
		},
		{
			name: "AWS static", providerType: "bedrock", params: model.LLMProviderParams{AWSRegion: "us-east-1"},
			credential: model.LLMCredentialInput{
				Mode: "aws-static", AccessKeyID: "AWS_TEST_ACCESS_ID",
				SecretAccessKey: "write-only-aws-secret", SessionToken: "write-only-session-token",
			},
			kind:      "aws-static",
			expected:  []string{`"aws"`, `"accessKeyId":"AWS_TEST_ACCESS_ID"`, `"secretAccessKey":"write-only-aws-secret"`, `"region":null`, `"serviceName":null`, `"sessionToken":"write-only-session-token"`},
			forbidden: []string{"AWS_TEST_ACCESS_ID", "write-only-aws-secret", "write-only-session-token"},
		},
		{
			name: "GCP file", providerType: "vertex", params: model.LLMProviderParams{VertexProject: "project-a"},
			credential: model.LLMCredentialInput{Mode: "gcp-file", Reference: "/var/run/gcp.json"},
			kind:       "gcp-file", expected: []string{`"gcp"`, `"file":"/var/run/gcp.json"`},
			forbidden: []string{"/var/run/gcp.json"},
		},
		{
			name: "Azure managed identity", providerType: "azure",
			params:     model.LLMProviderParams{AzureResourceName: "resource-a", AzureResourceType: "openAI"},
			credential: model.LLMCredentialInput{Mode: "azure-managed-identity", ClientID: "managed-client-id"},
			kind:       "azure-managed-identity",
			expected:   []string{`"azure"`, `"managedIdentity"`, `"clientId":"managed-client-id"`},
			forbidden:  []string{"managed-client-id"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &mutableConfigServer{config: json.RawMessage(`{"llm":{"providers":[],"models":[],"virtualModels":[]}}`)}
			upstream := httptest.NewServer(http.HandlerFunc(state.handler))
			defer upstream.Close()
			client, err := New(upstream.URL, upstream.Client(), 0)
			if err != nil {
				t.Fatal(err)
			}
			_, revision, err := client.LLMConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
				Operation: "create-llm-provider",
				Provider: model.LLMProviderDraft{
					Name: "provider-a", ProviderType: test.providerType, Params: test.params,
					Formats: []model.LLMProviderFormat{}, Credential: test.credential,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			state.mu.Lock()
			posted := string(state.lastPost)
			state.mu.Unlock()
			for _, expected := range test.expected {
				if !strings.Contains(posted, expected) {
					t.Fatalf("upstream write missing expected field %q", expected)
				}
			}
			configuration, _, err := client.LLMConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(configuration.Providers) != 1 || configuration.Providers[0].Credential.Kind != test.kind {
				t.Fatalf("credential state = %#v", configuration.Providers)
			}
			encoded, _ := json.Marshal(configuration)
			for _, forbidden := range test.forbidden {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("sanitized configuration exposed submitted credential field %q", forbidden)
				}
			}
		})
	}
}

func TestModelOutgoingModesWriteStripAndCustomExpressions(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(`{"llm":{"providers":[],"models":[],"virtualModels":[]}}`)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "create-llm-model",
		Model: model.LLMModelDraft{
			Name: "anthropic/*", ProviderMode: "builtin", ProviderType: "anthropic",
			Params: model.LLMProviderParams{}, Formats: []model.LLMProviderFormat{}, Visibility: "public",
			UpstreamMode: "strip", Credential: model.LLMCredentialInput{Mode: "ambient"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	posted := string(state.lastPost)
	state.mu.Unlock()
	if !strings.Contains(posted, `"model":"llmRequest.model.stripPrefix(\"anthropic/\")"`) {
		t.Fatalf("strip transformation missing: %s", posted)
	}

	configuration, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Models[0].UpstreamMode != "strip" {
		t.Fatalf("strip setting = %#v", configuration.Models[0])
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "update-llm-model", ResourceID: configuration.Models[0].ID,
		Model: model.LLMModelDraft{
			Name: "anthropic/*", ProviderMode: "builtin", ProviderType: "anthropic",
			Params: model.LLMProviderParams{}, Formats: []model.LLMProviderFormat{}, Visibility: "public",
			UpstreamMode: "custom", ModelExpression: `llmRequest.model + "-latest"`,
			Credential: model.LLMCredentialInput{Mode: "preserve"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, _, err = client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Models[0].UpstreamMode != "custom" || configuration.Models[0].ModelExpression != `llmRequest.model + "-latest"` {
		t.Fatalf("custom setting = %#v", configuration.Models[0])
	}
}

func TestAmbientCredentialRemovesAuthAndAPIKeyButPreservesOtherDefaults(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(`{
      "llm":{"providers":[{
        "name":"shared-openai","provider":"openai",
        "params":{"apiKey":"remove-api-key","model":"old-model"},
        "defaults":{"auth":{"basic":{"username":"remove-auth"}},"temperature":0.2}
      }],"models":[],"virtualModels":[]}
    }`)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	configuration, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "update-llm-provider", ResourceID: configuration.Providers[0].ID,
		Provider: model.LLMProviderDraft{
			Name: "shared-openai", ProviderType: "openai",
			Params:  model.LLMProviderParams{Model: "new-model"},
			Formats: []model.LLMProviderFormat{}, Credential: model.LLMCredentialInput{Mode: "ambient"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	posted := string(state.lastPost)
	for _, removed := range []string{"remove-api-key", "remove-auth", `"apiKey"`, `"auth"`} {
		if strings.Contains(posted, removed) {
			t.Fatalf("ambient write retained %q: %s", removed, posted)
		}
	}
	for _, retained := range []string{`"temperature":0.2`, `"model":"new-model"`} {
		if !strings.Contains(posted, retained) {
			t.Fatalf("ambient write removed %q: %s", retained, posted)
		}
	}
}

func TestStructuredCloudProviderRejectsAPIKeyReference(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(`{"llm":{"providers":[],"models":[],"virtualModels":[]}}`)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "create-llm-provider",
		Provider: model.LLMProviderDraft{
			Name: "aws", ProviderType: "bedrock", Formats: []model.LLMProviderFormat{},
			Credential: model.LLMCredentialInput{Mode: "environment", Reference: "AWS_ACCESS_KEY_ID"},
		},
	})
	if !errors.Is(err, ErrLLMInvalidRequest) {
		t.Fatalf("cloud credential error = %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.postCount != 0 {
		t.Fatalf("POST count = %d", state.postCount)
	}
}

func TestProviderSpecificParamsAreRejectedForOtherTypes(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(`{"llm":{"providers":[],"models":[],"virtualModels":[]}}`)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := client.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyLLMChange(t.Context(), revision, model.LLMChange{
		Operation: "create-llm-provider",
		Provider: model.LLMProviderDraft{
			Name: "openai", ProviderType: "openai",
			Params:  model.LLMProviderParams{VertexProject: "wrong-provider"},
			Formats: []model.LLMProviderFormat{}, Credential: model.LLMCredentialInput{Mode: "ambient"},
		},
	})
	if !errors.Is(err, ErrLLMInvalidRequest) {
		t.Fatalf("provider params error = %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.postCount != 0 {
		t.Fatalf("POST count = %d", state.postCount)
	}
}
