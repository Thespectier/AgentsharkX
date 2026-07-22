package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const populatedConfig = `{
  "llm": {
    "providers": [{"name":"openai-shared","provider":"openai","params":{"apiKey":"never-leak-me"}}],
    "models": [
      {"name":"openai/*","provider":"openai","params":{"apiKey":"also-secret"}},
      {"name":"fast","provider":{"reference":"openai-shared"},"params":{"model":"gpt-5.4-nano"}}
    ],
    "virtualModels": [{"name":"resilient","routing":{"failover":{"targets":[{"model":"fast","priority":0}]}}}]
  },
  "mcp": {"targets":[{"name":"everything","mcp":{"host":"http://localhost:3001/mcp"}}]},
  "binds":[{"port":8080,"listeners":[{"name":"public-http","hostname":"example.com","protocol":"HTTP","routes":[{"name":"api","matches":[{"path":{"pathPrefix":"/api"}}],"backends":[{"host":"localhost:9000"}]}]}]}]
}`

func TestHealthUsesVerifiedRuntimeContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"build":{"version":"1.3.1","gitRevision":"dbaaf7ed73671e7aec9195e35e7f726c0b14b84a"},"ui":{"gatewayMode":"standalone"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	health := client.Health(t.Context())
	if health.Source != model.SourceAgentGateway || health.Status != model.HealthHealthy || health.Version != "1.3.1" {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func TestCapabilitiesAreIndependentlyProbed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/runtime", "/api/config", "/api/costs/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case "/config_dump":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, &http.Client{Timeout: time.Second}, 0)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := client.Capabilities(t.Context())
	statuses := make(map[string]model.CapabilityStatus, len(capabilities))
	for _, capability := range capabilities {
		statuses[capability.ID] = capability.Status
	}
	if statuses["gateway.runtime"] != model.CapabilitySupported {
		t.Fatalf("runtime status = %q", statuses["gateway.runtime"])
	}
	if statuses["gateway.configuration"] != model.CapabilityPartial {
		t.Fatalf("configuration status = %q", statuses["gateway.configuration"])
	}
	if statuses["gateway.cost-catalog"] != model.CapabilitySupported {
		t.Fatalf("cost catalog status = %q", statuses["gateway.cost-catalog"])
	}
}

func TestSnapshotUsesOnlyVerifiedSafeConfigFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/config" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, populatedConfig)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Providers) != 1 || snapshot.Providers[0].ModelCount != 1 {
		t.Fatalf("unexpected providers: %#v", snapshot.Providers)
	}
	if snapshot.Providers[0].Source != model.SourceAgentGateway || snapshot.Providers[0].FetchedAt.IsZero() || snapshot.Providers[0].RawRef.ID != "/llm/providers/0" {
		t.Fatalf("source metadata was not preserved: %#v", snapshot.Providers[0])
	}
	if len(snapshot.Models) != 3 || snapshot.Models[1].TargetModel != "gpt-5.4-nano" || snapshot.Models[2].Routing != "failover" {
		t.Fatalf("unexpected models: %#v", snapshot.Models)
	}
	if len(snapshot.MCP) != 1 || snapshot.MCP[0].Transport != "mcp" || len(snapshot.Routes) != 1 || snapshot.Routes[0].BackendCount != 1 {
		t.Fatalf("unexpected resources: mcp=%#v routes=%#v", snapshot.MCP, snapshot.Routes)
	}
	encoded, _ := json.Marshal(snapshot)
	for _, forbidden := range []string{"never-leak-me", "also-secret", "apiKey"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestSnapshotFailsClearlyWhenPinnedContractChanges(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"llm":{"providers":[{"provider":"openai"}],"models":[]}}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Snapshot(t.Context())
	var contractError *ContractError
	if !errors.As(err, &contractError) || contractError.Field != "/llm/providers/0/name" {
		t.Fatalf("expected field-scoped contract error, got %v", err)
	}
}

func TestAnalyticsUsesVerifiedReadOnlyPostAndNormalizesMissingDatabase(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/logs/analytics/summary" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"bucketCount":12}` {
			t.Fatalf("unexpected analytics body: %s", body)
		}
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(writer, `{"error":"request log database is not configured"}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	analytics, err := client.Analytics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Status != "unavailable" || analytics.Requests != nil || len(analytics.Buckets) != 0 {
		t.Fatalf("unexpected analytics unavailable state: %#v", analytics)
	}
}

func TestAnalyticsFailsClearlyWhenBucketContractChanges(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"bucketSeconds":300,"buckets":[{"start":"2026-07-22T08:00:00Z","requests":"changed","totalTokens":1,"cost":0.1}]}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Analytics(t.Context())
	var contractError *ContractError
	if !errors.As(err, &contractError) || !strings.Contains(contractError.Field, "requests") {
		t.Fatalf("expected analytics field-scoped contract error, got %v", err)
	}
}

func TestAnalyticsSumsVerifiedBuckets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"bucketSeconds":300,"buckets":[{"start":"2026-07-22T08:00:00Z","requests":2,"totalTokens":20,"cost":0.1},{"start":"2026-07-22T08:05:00Z","requests":3,"totalTokens":30,"cost":0.2}]}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	analytics, err := client.Analytics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Status != "available" || analytics.Requests == nil || *analytics.Requests != 5 || analytics.TotalTokens == nil || *analytics.TotalTokens != 50 {
		t.Fatalf("unexpected analytics summary: %#v", analytics)
	}
}

func TestAnalyticsMissingRouteIsExplicitlyUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	analytics, err := client.Analytics(t.Context())
	if err != nil || analytics.Status != "unavailable" || analytics.Requests != nil {
		t.Fatalf("unexpected missing capability state: analytics=%#v err=%v", analytics, err)
	}
}
