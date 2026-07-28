package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const editableMCPConfig = `{
  "config":{"database":{"url":"sqlite://request-logs.db"}},
  "llm":{"providers":[{"name":"shared","provider":"openai"}],"models":[],"virtualModels":[]},
  "mcp":{
    "port":3000,
    "statefulMode":"stateful",
    "prefixMode":"conditional",
    "failureMode":"failOpen",
    "policies":{"cors":{"allowOrigins":["https://console.example"]}},
    "targets":[
      {"name":"weather","mcp":{"host":"https://weather.example/mcp"},"policies":{"requestHeaderModifier":{"set":{"x-tenant":"admin"}}}},
      {"name":"events","sse":{"host":"events.internal","port":8080,"path":"/sse"}},
      {"name":"filesystem","stdio":{"cmd":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/workspace"],"env":{"ACCESS_TOKEN":"complete-value","LOG_LEVEL":"info"},"clear_env":true}},
      {"name":"catalog","openapi":{"host":"https://catalog.example/api","schema":{"url":"https://catalog.example/openapi.json"}}}
    ]
  },
  "binds":[{"port":8080,"listeners":[{"name":"public","routes":[{"name":"tools","backends":[{"mcp":{"targets":[{"name":"inline","mcp":{"host":"https://inline.example/mcp"}}]}}]}]}]}]
}`

func TestMCPConfigurationReturnsCompleteVerifiedSettingsAndTargets(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableMCPConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if revision == "" || len(configuration.Servers) != 4 || len(configuration.InlineServers) != 1 {
		t.Fatalf("unexpected MCP configuration: %#v revision=%q", configuration, revision)
	}
	if configuration.Settings.Port == nil || *configuration.Settings.Port != 3000 ||
		configuration.Settings.StatefulMode != "stateful" || configuration.Settings.PrefixMode != "conditional" ||
		configuration.Settings.FailureMode != "failOpen" || !configuration.Settings.HasPolicies {
		t.Fatalf("unexpected MCP settings: %#v", configuration.Settings)
	}
	if configuration.Servers[0].Network == nil || configuration.Servers[0].Network.Mode != "url" || !configuration.Servers[0].HasPolicies {
		t.Fatalf("unexpected network target: %#v", configuration.Servers[0])
	}
	stdio := configuration.Servers[2].Stdio
	if stdio == nil || stdio.Environment["ACCESS_TOKEN"] != "complete-value" || !stdio.ClearEnvironment || len(stdio.Arguments) != 3 {
		t.Fatalf("unexpected stdio target: %#v", stdio)
	}
	if configuration.Servers[3].Editable || configuration.Servers[3].Transport != "openapi" {
		t.Fatalf("OpenAPI target must remain advanced-only: %#v", configuration.Servers[3])
	}
}

func TestApplyMCPChangePreservesPoliciesAndUnrelatedConfiguration(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableMCPConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	target, err := client.ApplyMCPChange(t.Context(), revision, model.MCPChange{
		Operation: "update-mcp-server", ResourceID: configuration.Servers[0].ID,
		Server: model.MCPServerDraft{
			Name: "weather", Transport: "mcp",
			Network: &model.MCPNetworkTarget{Mode: "host", Host: "weather.internal", Port: intPointer(9443), Path: "/mcp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "weather" {
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
		"sqlite://request-logs.db", `"provider":"openai"`, "console.example", "x-tenant",
		"complete-value", "openapi.json", "inline.example",
	} {
		if !strings.Contains(string(posted), preserved) {
			t.Fatalf("write did not preserve %q: %s", preserved, posted)
		}
	}
	for _, applied := range []string{"weather.internal", `"port":9443`, `"path":"/mcp"`} {
		if !strings.Contains(string(posted), applied) {
			t.Fatalf("write did not apply %q: %s", applied, posted)
		}
	}
}

func TestApplyMCPSettingsCreateAndDeleteUseRevisionControlledSingleWrites(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableMCPConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyMCPChange(t.Context(), "stale", model.MCPChange{
		Operation: "delete-mcp-server", ResourceID: configuration.Servers[0].ID,
	})
	if !errors.Is(err, ErrConfigurationChanged) {
		t.Fatalf("stale revision error = %v", err)
	}
	_, err = client.ApplyMCPChange(t.Context(), revision, model.MCPChange{
		Operation: "create-mcp-server",
		Server: model.MCPServerDraft{
			Name: "search", Transport: "sse",
			Network: &model.MCPNetworkTarget{Mode: "url", Host: "https://search.example/sse"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, revision, err = client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var search model.MCPServerSetting
	for _, server := range configuration.Servers {
		if server.Name == "search" {
			search = server
		}
	}
	if search.ID == "" {
		t.Fatal("created MCP target was not returned")
	}
	_, err = client.ApplyMCPChange(t.Context(), revision, model.MCPChange{
		Operation: "delete-mcp-server", ResourceID: search.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, revision, err = client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyMCPChange(t.Context(), revision, model.MCPChange{
		Operation: "update-mcp-settings",
		Settings: model.MCPGlobalSettingsDraft{
			Port: intPointer(3100), StatefulMode: "stateless", PrefixMode: "none", FailureMode: "failClosed",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.postCount != 3 {
		t.Fatalf("POST count = %d", state.postCount)
	}
	if !strings.Contains(string(state.lastPost), `"port":3100`) || !strings.Contains(string(state.lastPost), `"prefixMode":null`) ||
		!strings.Contains(string(state.lastPost), "console.example") {
		t.Fatalf("unexpected settings write: %s", state.lastPost)
	}
}

func TestApplyMCPChangeRejectsUnverifiedAndAdvancedTargetWrites(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableMCPConfig), ignorePost: true}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyMCPChange(t.Context(), revision, model.MCPChange{
		Operation: "update-mcp-server", ResourceID: configuration.Servers[0].ID,
		Server: model.MCPServerDraft{
			Name: "weather", Transport: "mcp",
			Network: &model.MCPNetworkTarget{Mode: "url", Host: "https://changed.example/mcp"},
		},
	})
	if !errors.Is(err, ErrMCPWriteUnverified) {
		t.Fatalf("unverified write error = %v", err)
	}

	state.ignorePost = false
	configuration, revision, err = client.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyMCPChange(t.Context(), revision, model.MCPChange{
		Operation: "delete-mcp-server", ResourceID: configuration.Servers[3].ID,
	})
	if !errors.Is(err, ErrMCPInvalidRequest) {
		t.Fatalf("advanced target deletion error = %v", err)
	}
}

func intPointer(value int) *int { return &value }
