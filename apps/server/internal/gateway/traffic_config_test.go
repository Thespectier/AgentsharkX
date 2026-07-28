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

const editableTrafficConfig = `{
  "config":{"database":{"url":"sqlite://request-logs.db"}},
  "llm":{"providers":[{"name":"shared","provider":"openai"}],"models":[],"virtualModels":[]},
  "mcp":{"targets":[{"name":"weather","mcp":{"host":"https://weather.example/mcp"}}]},
  "binds":[
    {"port":8080,"tunnelProtocol":"direct","listeners":[
      {"name":"public-http","hostname":"example.com","protocol":"HTTPS",
       "tls":{"mode":"dynamicCa","cert":"/cert.pem","key":"/key.pem","minTLSVersion":"TLS_V1_2"},
       "policies":{"cors":{"allowOrigins":["https://console.example"]}},
       "routes":[
         {"name":"api","hostnames":["example.com"],
          "matches":[{"path":{"pathPrefix":"/api"},"method":"POST","headers":[{"name":"x-tenant","value":{"exact":"admin"}}],"query":[{"name":"debug","value":{"regex":"true|1"}}]}],
          "policies":{"timeout":{"requestTimeout":"30s"}},
          "backends":[
            {"host":"localhost:9000","weight":2,"policies":{"backendAuth":{"token":"complete-value"}}},
            {"dynamic":{}},
            {"routeGroup":"shared-routes"},
            {"aws":{"lambda":{"functionName":"verified-advanced"}}}
          ]}
       ]}
    ]},
    {"port":9090,"listeners":[
      {"name":"database","protocol":"TCP","tcpRoutes":[{"name":"mysql","hostnames":["db.example.com"],"backends":[{"service":{"name":"default/mysql","port":3306}}]}]}
    ]}
  ]
}`

func TestTrafficConfigurationReturnsCompleteVerifiedObjects(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableTrafficConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.TrafficConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if revision == "" || len(configuration.Binds) != 2 || len(configuration.Listeners) != 2 || len(configuration.Routes) != 2 {
		t.Fatalf("unexpected traffic configuration: %#v revision=%q", configuration, revision)
	}
	listener := configuration.Listeners[0]
	if listener.Protocol != "HTTPS" || listener.RouteCount != 1 || listener.BackendCount != 4 {
		t.Fatalf("unexpected listener: %#v", listener)
	}
	tls, ok := listener.Configuration["tls"].(map[string]any)
	if !ok || tls["mode"] != "dynamicCa" || tls["minTLSVersion"] != "TLS_V1_2" {
		t.Fatalf("complete TLS configuration missing: %#v", listener.Configuration)
	}
	route := configuration.Routes[0]
	if route.Kind != "http" || route.BackendCount != 4 {
		t.Fatalf("unexpected route: %#v", route)
	}
	if !strings.Contains(mustJSONString(t, route.Configuration), "complete-value") ||
		!strings.Contains(mustJSONString(t, route.Configuration), "verified-advanced") ||
		!strings.Contains(mustJSONString(t, route.Configuration), "true|1") {
		t.Fatalf("complete route configuration missing: %#v", route.Configuration)
	}
}

func TestApplyTrafficRouteChangePreservesUnrelatedConfiguration(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableTrafficConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.TrafficConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	route := configuration.Routes[0]
	next := cloneConfigObject(t, route.Configuration)
	next["name"] = "api-v2"
	next["backends"] = []any{map[string]any{"backend": "catalog", "weight": float64(3), "policies": map[string]any{"backendTLS": map[string]any{"insecure": true}}}}
	target, err := client.ApplyTrafficChange(t.Context(), revision, model.TrafficChange{
		Operation: "update-traffic-route", ResourceID: route.ID,
		Route: model.TrafficRouteDraft{Kind: "http", Configuration: next},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "api-v2" {
		t.Fatalf("target = %q", target)
	}
	state.mu.Lock()
	posted := string(state.lastPost)
	postCount := state.postCount
	state.mu.Unlock()
	if postCount != 1 {
		t.Fatalf("POST count = %d", postCount)
	}
	for _, preserved := range []string{"sqlite://request-logs.db", `"provider":"openai"`, "weather.example", "dynamicCa", "console.example", "default/mysql"} {
		if !strings.Contains(posted, preserved) {
			t.Fatalf("write did not preserve %q: %s", preserved, posted)
		}
	}
	for _, applied := range []string{"api-v2", `"backend":"catalog"`, `"weight":3`, `"insecure":true`} {
		if !strings.Contains(posted, applied) {
			t.Fatalf("write did not apply %q: %s", applied, posted)
		}
	}
}

func TestTrafficMutationsEnforceRevisionProtocolAndWriteVerification(t *testing.T) {
	t.Parallel()
	state := &mutableConfigServer{config: json.RawMessage(editableTrafficConfig)}
	upstream := httptest.NewServer(http.HandlerFunc(state.handler))
	defer upstream.Close()
	client, err := New(upstream.URL, upstream.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	configuration, revision, err := client.TrafficConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyTrafficChange(t.Context(), "stale", model.TrafficChange{
		Operation: "delete-traffic-route", ResourceID: configuration.Routes[0].ID,
	})
	if !errors.Is(err, ErrConfigurationChanged) {
		t.Fatalf("stale revision error = %v", err)
	}

	listener := configuration.Listeners[0]
	next := cloneConfigObject(t, listener.Configuration)
	next["protocol"] = "TCP"
	_, err = client.ApplyTrafficChange(t.Context(), revision, model.TrafficChange{
		Operation: "update-traffic-listener", ResourceID: listener.ID,
		Listener: model.TrafficListenerDraft{Configuration: next},
	})
	if !errors.Is(err, ErrTrafficResourceReferenced) {
		t.Fatalf("protocol switch error = %v", err)
	}

	_, err = client.ApplyTrafficChange(t.Context(), revision, model.TrafficChange{
		Operation: "delete-traffic-bind", ResourceID: configuration.Binds[0].ID,
	})
	if !errors.Is(err, ErrTrafficResourceReferenced) {
		t.Fatalf("unconfirmed cascading bind deletion error = %v", err)
	}
	_, err = client.ApplyTrafficChange(t.Context(), revision, model.TrafficChange{
		Operation: "delete-traffic-listener", ResourceID: listener.ID,
	})
	if !errors.Is(err, ErrTrafficResourceReferenced) {
		t.Fatalf("unconfirmed cascading listener deletion error = %v", err)
	}

	state.ignorePost = true
	configuration, revision, err = client.TrafficConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyTrafficChange(t.Context(), revision, model.TrafficChange{
		Operation: "create-traffic-bind", Bind: model.TrafficBindDraft{Port: 7070},
	})
	if !errors.Is(err, ErrTrafficWriteUnverified) {
		t.Fatalf("unverified write error = %v", err)
	}
}

func cloneConfigObject(t *testing.T, value model.TrafficConfigObject) model.TrafficConfigObject {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone model.TrafficConfigObject
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func mustJSONString(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
