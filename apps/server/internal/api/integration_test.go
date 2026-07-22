package api

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
)

func TestFakeUpstreamsRemainIndependentThroughBFF(t *testing.T) {
	t.Parallel()

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/runtime":
			_, _ = writer.Write([]byte(`{"build":{"version":"1.3.1"},"ui":{"gatewayMode":"standalone"}}`))
		case "/api/config", "/config_dump", "/api/costs/models":
			_, _ = writer.Write([]byte(`{}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer gatewayServer.Close()

	const guardSecret = "guard-secret-with-enough-entropy"
	guardServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != guardSecret {
			t.Errorf("guard API key missing from protected request")
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("sensitive-guard-response"))
	}))
	defer guardServer.Close()

	gatewayClient, err := gateway.New(gatewayServer.URL, gatewayServer.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	guardClient, err := guard.New(guardServer.URL, guardSecret, guardServer.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	aggregator := aggregate.New("integration", gatewayClient, guardClient)
	aggregator.Refresh(t.Context())

	handler := New(ServerConfig{
		Aggregate: aggregator, Stream: stream.NewHub(), Logger: slog.New(slog.DiscardHandler), AuthEnabled: false,
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/overview")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var overview model.OverviewEnvelope
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !overview.Meta.Partial {
		t.Fatalf("expected HTTP 200 partial overview: status=%d meta=%#v", response.StatusCode, overview.Meta)
	}
	if overview.Data.Health[0].Status != model.HealthHealthy || overview.Data.Health[1].Status != model.HealthDown {
		t.Fatalf("source independence lost: %#v", overview.Data.Health)
	}
	encoded, _ := json.Marshal(overview)
	for _, forbidden := range []string{guardSecret, "sensitive-guard-response"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("BFF response leaked %q", forbidden)
		}
	}
}

func TestStreamStartsWithNormalizedHealthEvents(t *testing.T) {
	t.Parallel()

	checkedAt := time.Now().UTC()
	aggregator := aggregate.New("test", apiFakeSource{model.SourceHealth{
		Source: model.SourceAgentGateway, Label: "agentgateway", Status: model.HealthHealthy, CheckedAt: checkedAt,
	}}, apiFakeSource{model.SourceHealth{
		Source: model.SourceAgentGuard, Label: "AgentGuard", Status: model.HealthDegraded, CheckedAt: checkedAt,
	}})
	aggregator.Refresh(t.Context())
	server := httptest.NewServer(New(ServerConfig{
		Aggregate: aggregator, Stream: stream.NewHub(), Logger: slog.New(slog.DiscardHandler), AuthEnabled: false,
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/stream", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q", contentType)
	}
	scanner := bufio.NewScanner(response.Body)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		if line == "" {
			break
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "event: health") || !strings.Contains(joined, `"source":"agentgateway"`) {
		t.Fatalf("unexpected initial SSE event: %s", joined)
	}
}

func TestConnectResourcesFlowThroughBFFWithFilteringAndDetails(t *testing.T) {
	t.Parallel()

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/config":
			_, _ = writer.Write([]byte(`{"llm":{"providers":[{"name":"shared","provider":"openai"}],"models":[{"name":"fast","provider":{"reference":"shared"}}]},"binds":[]}`))
		case "/api/runtime":
			_, _ = writer.Write([]byte(`{"build":{"version":"1.3.1"}}`))
		case "/api/logs/analytics/summary":
			_, _ = writer.Write([]byte(`{"bucketSeconds":300,"buckets":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer gatewayServer.Close()
	gatewayClient, err := gateway.New(gatewayServer.URL, gatewayServer.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(ServerConfig{
		Connect: connect.New(gatewayClient, "http://localhost:15000/ui"), Logger: slog.New(slog.DiscardHandler), AuthEnabled: false,
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/connect/llm/models?q=fast&limit=1")
	if err != nil {
		t.Fatal(err)
	}
	var page model.ResourcePageEnvelope[model.GatewayModel]
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || page.Data.Total != 1 || page.Data.Items[0].Provider != "reference:shared" {
		t.Fatalf("unexpected model page: status=%d page=%#v", response.StatusCode, page)
	}

	detailResponse, err := server.Client().Get(server.URL + "/api/v1/connect/llm/models/" + page.Data.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer detailResponse.Body.Close()
	if detailResponse.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d", detailResponse.StatusCode)
	}

	invalid, err := server.Client().Get(server.URL + "/api/v1/connect/llm/models?cursor=invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer invalid.Body.Close()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", invalid.StatusCode)
	}
}
