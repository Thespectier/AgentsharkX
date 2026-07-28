package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const completeGatewayPolicyConfig = `{
  "config":{"database":{"url":"sqlite://request-logs.db"}},
  "opaqueRoot":{"keep":{"nested":true}},
  "llm":{
    "providers":[{
      "name":"shared",
      "provider":"openai",
      "params":{"apiKey":"preserve-provider-secret","baseUrl":"https://provider.example.invalid"}
    }],
    "models":[
      {
        "name":"duplicate",
        "provider":"openai",
        "params":{"apiKey":"preserve-model-secret","model":"gpt-test"},
        "matches":[{"headers":[{"name":"x-tenant","exact":"primary"}]}],
        "authorization":{"rules":["jwt.sub != ''"]},
        "defaults":{"temperature":0.2},
        "overrides":{"stream":false},
        "transformation":{"user":"llmRequest.user"},
        "requestHeaders":{"set":{"x-policy-scope":"admin"}},
        "responseHeaders":{"remove":["server"]},
        "tls":{"root":"/certs/root.pem"},
        "auth":{"basic":{"username":"service","password":"preserve-backend-secret"}},
        "health":{"unhealthyExpression":"response.code >= 500"},
        "backendTunnel":{"proxy":{"host":"proxy.internal","port":8080}},
        "guardrails":{"request":[{"regex":{"patterns":["secret"]}}]},
        "promptCaching":{"cacheSystem":true,"minTokens":1024}
      },
      {
        "name":"duplicate",
        "provider":{"reference":"shared"},
        "backendTLS":{"insecure":true}
      }
    ],
    "virtualModels":[{"name":"virtual","routes":[{"model":"duplicate","weight":1}]}],
    "policies":{
      "cors":{"allowOrigins":["https://console.example.invalid"],"allowMethods":["POST"]},
      "authorization":{"rules":["jwt.email.endsWith('@example.invalid')"]},
      "localRateLimit":[{"type":"tokens","fillInterval":"1s","tokensPerFill":10,"maxTokens":100}],
      "futurePolicy":{"mode":"preserve","nested":{"enabled":true}}
    }
  },
  "mcp":{
    "port":3000,
    "targets":[{
      "name":"tools",
      "mcp":{"host":"localhost","port":3001},
      "policies":{"authorization":{"rules":["true"]}}
    }],
    "policies":{
      "cors":{"allowOrigins":["https://console.example.invalid"],"exposeHeaders":["Mcp-Session-Id"]},
      "mcpAuthorization":{"rules":["mcp.tool.name == 'get_weather'"]},
      "futureMcpPolicy":{"value":[1,2,3]}
    }
  },
  "binds":[{
    "port":8080,
    "listeners":[{
      "protocol":"HTTP",
      "routes":[{"backends":[{"host":"backend.internal","policies":{"timeout":"5s"}}]}]
    }]
  }]
}`

func TestPolicyConfigurationReturnsCompleteCatalogsAndSourceValues(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(completeGatewayPolicyConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.PolicyConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if revision == "" || configuration.Source != model.SourceAgentGateway || configuration.FetchedAt.IsZero() {
		t.Fatalf("unexpected configuration metadata: %#v revision=%q", configuration, revision)
	}
	if got, want := len(configuration.Settings), 51; got != want {
		t.Fatalf("settings=%d, want %d", got, want)
	}

	llmCors := gatewayPolicyAtPath(t, configuration.Settings, "/llm/policies/cors")
	assertPolicyJSON(t, llmCors.Value, `{"allowOrigins":["https://console.example.invalid"],"allowMethods":["POST"]}`)
	if !llmCors.Enabled || !llmCors.Editable || llmCors.Family != "llm" || llmCors.Group != "Access" {
		t.Fatalf("unexpected LLM CORS setting: %#v", llmCors)
	}
	if llmCors.Source != model.SourceAgentGateway || llmCors.UpstreamID != llmCors.RawRef.ID || llmCors.RawRef.ID != "/llm/policies/cors" {
		t.Fatalf("source identity was not preserved: %#v", llmCors.ProtectResourceBase)
	}

	disabledJWT := gatewayPolicyAtPath(t, configuration.Settings, "/llm/policies/jwtAuth")
	if disabledJWT.Enabled || disabledJWT.Value != nil || !disabledJWT.Editable {
		t.Fatalf("known disabled policy was not returned correctly: %#v", disabledJWT)
	}
	unknown := gatewayPolicyAtPath(t, configuration.Settings, "/llm/policies/futurePolicy")
	if !unknown.Enabled || unknown.Editable || unknown.Group != "Other" {
		t.Fatalf("unknown policy must be visible and uneditable: %#v", unknown)
	}
	assertPolicyJSON(t, unknown.Value, `{"mode":"preserve","nested":{"enabled":true}}`)

	modelAuthorization := gatewayPolicyAtPath(t, configuration.Settings, "/llm/models/0/authorization")
	assertPolicyJSON(t, modelAuthorization.Value, `{"rules":["jwt.sub != ''"]}`)
	modelGuardrails := gatewayPolicyAtPath(t, configuration.Settings, "/llm/models/0/guardrails")
	assertPolicyJSON(t, modelGuardrails.Value, `{"request":[{"regex":{"patterns":["secret"]}}]}`)
	modelTLSAlias := gatewayPolicyAtPath(t, configuration.Settings, "/llm/models/1/backendTLS")
	assertPolicyJSON(t, modelTLSAlias.Value, `{"insecure":true}`)
	firstID := gatewayPolicyAtPath(t, configuration.Settings, "/llm/models/0/defaults").ID
	secondID := gatewayPolicyAtPath(t, configuration.Settings, "/llm/models/1/defaults").ID
	if firstID == secondID || modelAuthorization.Target != "duplicate" || modelTLSAlias.Target != "duplicate" {
		t.Fatalf("model policy identity must use array index even when names repeat")
	}

	mcpAuthorization := gatewayPolicyAtPath(t, configuration.Settings, "/mcp/policies/mcpAuthorization")
	assertPolicyJSON(t, mcpAuthorization.Value, `{"rules":["mcp.tool.name == 'get_weather'"]}`)
	unknownMCP := gatewayPolicyAtPath(t, configuration.Settings, "/mcp/policies/futureMcpPolicy")
	if unknownMCP.Editable || !unknownMCP.Enabled || unknownMCP.Family != "mcp" {
		t.Fatalf("unexpected unknown MCP policy: %#v", unknownMCP)
	}
}

func TestPolicyConfigurationRevisionUsesCanonicalWholeDocument(t *testing.T) {
	t.Parallel()
	configurations := []json.RawMessage{
		json.RawMessage(`{"llm":{"models":[],"policies":{"cors":{"allowMethods":["POST"],"allowOrigins":["https://console.example.invalid"]}}},"mcp":{"targets":[]},"opaque":{"count":9007199254740993}}`),
		json.RawMessage(`{
          "opaque": {"count": 9007199254740993},
          "mcp": {"targets": []},
          "llm": {"policies": {"cors": {"allowOrigins": ["https://console.example.invalid"], "allowMethods": ["POST"]}}, "models": []}
        }`),
	}
	revisions := make([]string, 0, len(configurations))
	for _, configuration := range configurations {
		state := &mutableConfigServer{config: configuration}
		upstream := httptest.NewServer(http.HandlerFunc(state.handler))
		client, err := New(upstream.URL, upstream.Client(), 0)
		if err != nil {
			upstream.Close()
			t.Fatal(err)
		}
		_, revision, err := client.PolicyConfiguration(t.Context())
		upstream.Close()
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, revision)
	}
	if revisions[0] == "" || revisions[0] != revisions[1] {
		t.Fatalf("semantically identical documents produced revisions %q and %q", revisions[0], revisions[1])
	}
}

func TestApplyPolicyChangeUpsertsEachVerifiedScopeAndPreservesWholeDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		path  string
		value any
	}{
		{"LLM global", "/llm/policies/jwtAuth", map[string]any{"mode": "strict", "issuer": "https://issuer.example.invalid"}},
		{"direct model", "/llm/models/1/requestHeaders", map[string]any{"set": map[string]any{"x-upstream": "model-two"}}},
		{"MCP global", "/mcp/policies/mcpAuthentication", map[string]any{"issuer": "https://mcp.example.invalid", "audiences": []any{"tools"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &mutableConfigServer{config: json.RawMessage(completeGatewayPolicyConfig)}
			upstream := httptest.NewServer(http.HandlerFunc(state.handler))
			defer upstream.Close()
			client, err := New(upstream.URL, upstream.Client(), 0)
			if err != nil {
				t.Fatal(err)
			}
			configuration, revision, err := client.PolicyConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			setting := gatewayPolicyAtPath(t, configuration.Settings, test.path)
			target, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
				Operation: changeUpsertGatewayPolicy, ResourceID: setting.ID, Value: test.value,
			})
			if err != nil {
				t.Fatal(err)
			}
			if target != test.path || state.postCount != 1 {
				t.Fatalf("target=%q postCount=%d", target, state.postCount)
			}
			assertPolicyJSON(t, postedValueAtPointer(t, state.lastPost, test.path), mustPolicyJSON(t, test.value))
			assertPreservedPolicyConfiguration(t, state.lastPost)
		})
	}
}

func TestApplyPolicyChangeDeletesOneFieldAndReturnsItDisabled(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(completeGatewayPolicyConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	configuration, revision, err := client.PolicyConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	setting := gatewayPolicyAtPath(t, configuration.Settings, "/llm/models/0/defaults")
	if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
		Operation: changeDeleteGatewayPolicy, ResourceID: setting.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if state.postCount != 1 || pointerExists(t, state.lastPost, setting.RawRef.ID) {
		t.Fatalf("delete did not remove exactly the selected field")
	}
	assertPreservedPolicyConfiguration(t, state.lastPost)
	after, _, err := client.PolicyConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	deleted := gatewayPolicyAtPath(t, after.Settings, setting.RawRef.ID)
	if deleted.Enabled || deleted.Value != nil || !deleted.Editable {
		t.Fatalf("deleted catalog entry should remain visible as disabled: %#v", deleted)
	}
}

func TestApplyPolicyChangeUsesNativeGlobalDisableRepresentation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		path       string
		wantExists bool
		wantValue  any
	}{
		{name: "LLM policies retain a null marker", path: "/llm/policies/cors", wantExists: true},
		{name: "LLM local rate limit is removed", path: "/llm/policies/localRateLimit"},
		{name: "MCP policies are removed", path: "/mcp/policies/cors"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &mutableConfigServer{config: json.RawMessage(completeGatewayPolicyConfig)}
			upstream := httptest.NewServer(http.HandlerFunc(state.handler))
			defer upstream.Close()
			client, err := New(upstream.URL, upstream.Client(), 0)
			if err != nil {
				t.Fatal(err)
			}
			configuration, revision, err := client.PolicyConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			setting := gatewayPolicyAtPath(t, configuration.Settings, test.path)
			if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
				Operation: changeDeleteGatewayPolicy, ResourceID: setting.ID,
			}); err != nil {
				t.Fatal(err)
			}
			if got := pointerExists(t, state.lastPost, test.path); got != test.wantExists {
				t.Fatalf("path exists=%v, want %v", got, test.wantExists)
			}
			if test.wantExists {
				if value := postedValueAtPointer(t, state.lastPost, test.path); value != test.wantValue {
					t.Fatalf("disabled policy value=%#v, want %#v", value, test.wantValue)
				}
			}
			if state.postCount != 1 {
				t.Fatalf("disable posted %d times", state.postCount)
			}
		})
	}
}

func TestApplyPolicyChangeCreatesVerifiedGlobalScopesWithNativeDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		path          string
		requiredArray string
	}{
		{"LLM", "/llm/policies/cors", "/llm/models"},
		{"MCP", "/mcp/policies/cors", "/mcp/targets"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &mutableConfigServer{config: json.RawMessage(`{"opaqueRoot":{"keep":true}}`)}
			upstream := httptest.NewServer(http.HandlerFunc(state.handler))
			defer upstream.Close()
			client, err := New(upstream.URL, upstream.Client(), 0)
			if err != nil {
				t.Fatal(err)
			}
			configuration, revision, err := client.PolicyConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			setting := gatewayPolicyAtPath(t, configuration.Settings, test.path)
			if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
				Operation: changeUpsertGatewayPolicy, ResourceID: setting.ID,
				Value: map[string]any{"allowOrigins": []any{"https://console.example.invalid"}},
			}); err != nil {
				t.Fatal(err)
			}
			value := postedValueAtPointer(t, state.lastPost, test.requiredArray)
			array, ok := value.([]any)
			if !ok || len(array) != 0 {
				t.Fatalf("%s=%#v, want empty array", test.requiredArray, value)
			}
			if keep := postedValueAtPointer(t, state.lastPost, "/opaqueRoot/keep"); keep != true {
				t.Fatalf("unowned root field changed: %#v", keep)
			}
		})
	}
}

func TestApplyPolicyChangeRejectsStaleInvalidAndUnverifiedWrites(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(completeGatewayPolicyConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	configuration, revision, err := client.PolicyConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	setting := gatewayPolicyAtPath(t, configuration.Settings, "/llm/policies/cors")
	if _, err := client.ApplyPolicyChange(t.Context(), "stale", model.GatewayPolicyChange{
		Operation: changeDeleteGatewayPolicy, ResourceID: setting.ID,
	}); !errors.Is(err, ErrConfigurationChanged) {
		t.Fatalf("stale revision error=%v", err)
	}
	if state.postCount != 0 {
		t.Fatalf("stale request posted %d times", state.postCount)
	}

	unknown := gatewayPolicyAtPath(t, configuration.Settings, "/llm/policies/futurePolicy")
	if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
		Operation: changeUpsertGatewayPolicy, ResourceID: unknown.ID, Value: map[string]any{"changed": true},
	}); !errors.Is(err, ErrGatewayPolicyInvalidRequest) {
		t.Fatalf("unknown policy write error=%v", err)
	}
	if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
		Operation: changeUpsertGatewayPolicy, ResourceID: setting.ID,
	}); !errors.Is(err, ErrGatewayPolicyInvalidRequest) {
		t.Fatalf("nil value error=%v", err)
	}
	disabled := gatewayPolicyAtPath(t, configuration.Settings, "/llm/policies/jwtAuth")
	if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
		Operation: changeDeleteGatewayPolicy, ResourceID: disabled.ID,
	}); !errors.Is(err, ErrGatewayPolicyNotFound) {
		t.Fatalf("disabled delete error=%v", err)
	}
	if state.postCount != 0 {
		t.Fatalf("invalid requests posted %d times", state.postCount)
	}

	state.ignorePost = true
	if _, err := client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
		Operation: changeUpsertGatewayPolicy, ResourceID: setting.ID,
		Value: map[string]any{"allowOrigins": []any{"https://changed.example.invalid"}},
	}); !errors.Is(err, ErrGatewayPolicyWriteUnverified) {
		t.Fatalf("unverified write error=%v", err)
	}
	if state.postCount != 1 {
		t.Fatalf("unverified write posted %d times", state.postCount)
	}
}

func TestApplyPolicyChangeRejectsTLSCompatibilityAliasConflict(t *testing.T) {
	t.Parallel()
	paths := []string{"/llm/models/0/backendTLS", "/llm/models/1/tls"}
	for _, path := range paths {
		state := &mutableConfigServer{config: json.RawMessage(completeGatewayPolicyConfig)}
		upstream := httptest.NewServer(http.HandlerFunc(state.handler))
		client, err := New(upstream.URL, upstream.Client(), 0)
		if err != nil {
			upstream.Close()
			t.Fatal(err)
		}
		configuration, revision, err := client.PolicyConfiguration(t.Context())
		if err != nil {
			upstream.Close()
			t.Fatal(err)
		}
		alias := gatewayPolicyAtPath(t, configuration.Settings, path)
		_, err = client.ApplyPolicyChange(t.Context(), revision, model.GatewayPolicyChange{
			Operation: changeUpsertGatewayPolicy, ResourceID: alias.ID, Value: map[string]any{"insecure": true},
		})
		upstream.Close()
		if !errors.Is(err, ErrGatewayPolicyInvalidRequest) {
			t.Fatalf("TLS alias conflict for %s error=%v", path, err)
		}
		if state.postCount != 0 {
			t.Fatalf("TLS alias conflict for %s posted %d times", path, state.postCount)
		}
	}
}

func gatewayPolicyAtPath(t *testing.T, settings []model.GatewayPolicySetting, path string) model.GatewayPolicySetting {
	t.Helper()
	for _, setting := range settings {
		if setting.RawRef.ID == path {
			return setting
		}
	}
	t.Fatalf("policy setting %s not found", path)
	return model.GatewayPolicySetting{}
}

func assertPolicyJSON(t *testing.T, actual any, expected string) {
	t.Helper()
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	var actualValue any
	if err := json.Unmarshal(actualJSON, &actualValue); err != nil {
		t.Fatal(err)
	}
	var expectedValue any
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Fatalf("value=%#v, want %#v", actualValue, expectedValue)
	}
}

func mustPolicyJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertPreservedPolicyConfiguration(t *testing.T, payload json.RawMessage) {
	t.Helper()
	checks := map[string]any{
		"/config/database/url":                                      "sqlite://request-logs.db",
		"/opaqueRoot/keep/nested":                                   true,
		"/llm/providers/0/params/apiKey":                            "preserve-provider-secret",
		"/llm/models/0/params/apiKey":                               "preserve-model-secret",
		"/llm/models/0/matches/0/headers/0/exact":                   "primary",
		"/llm/models/0/auth/basic/password":                         "preserve-backend-secret",
		"/llm/virtualModels/0/name":                                 "virtual",
		"/llm/policies/futurePolicy/nested/enabled":                 true,
		"/mcp/policies/futureMcpPolicy/value/2":                     float64(3),
		"/mcp/targets/0/name":                                       "tools",
		"/mcp/targets/0/policies/authorization/rules/0":             "true",
		"/binds/0/listeners/0/routes/0/backends/0/host":             "backend.internal",
		"/binds/0/listeners/0/routes/0/backends/0/policies/timeout": "5s",
	}
	for path, expected := range checks {
		if actual := postedValueAtPointer(t, payload, path); !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s=%#v, want %#v", path, actual, expected)
		}
	}
}

func postedValueAtPointer(t *testing.T, payload json.RawMessage, pointer string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	current := value
	for _, rawPart := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(rawPart, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				t.Fatalf("JSON pointer %s is missing at %s", pointer, part)
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				t.Fatalf("JSON pointer %s has invalid index %s", pointer, part)
			}
			current = typed[index]
		default:
			t.Fatalf("JSON pointer %s cannot descend through %#v", pointer, current)
		}
	}
	return current
}

func pointerExists(t *testing.T, payload json.RawMessage, pointer string) bool {
	t.Helper()
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	current := value
	for _, rawPart := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(rawPart, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return false
			}
			current = typed[index]
		default:
			return false
		}
	}
	return true
}
