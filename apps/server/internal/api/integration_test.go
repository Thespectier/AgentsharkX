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
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/trust"
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

func TestTrustResourcesLabelsAndScanFlowThroughAuthenticatedBFF(t *testing.T) {
	t.Parallel()

	const guardSecret = "guard-secret-with-enough-entropy"
	guardServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != guardSecret {
			t.Errorf("guard API key missing")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/backend/sessions":
			_, _ = writer.Write([]byte(`{"sessions":[{"session_id":"s-1","agent_id":"agent-a","user_id":"user-a","last_seen":1784688000}]}`))
		case "/v1/backend/tools":
			_, _ = writer.Write([]byte(`[{"owner_agent_id":"agent-a","name":"mail.send","labels":{"boundary":"internal","sensitivity":"low","integrity":"trusted","tags":[]}}]`))
		case "/v1/backend/skills":
			_, _ = writer.Write([]byte(`[{"owner_agent_id":"agent-a","agent_id":"agent-a","skill_unique_id":"skill-1","name":"research","source_framework":"langchain","detect_result":null}]`))
		case "/v1/backend/mcps":
			_, _ = writer.Write([]byte(`[]`))
		case "/v1/backend/agents/agent-a/tools/mail.send/labels":
			if request.Method != http.MethodPatch {
				t.Errorf("label method = %s", request.Method)
			}
			_, _ = writer.Write([]byte(`{"ok":true,"tool":{"owner_agent_id":"agent-a","name":"mail.send","labels":{"boundary":"server-confirmed","sensitivity":"low","integrity":"trusted","tags":[]}}}`))
		case "/v1/backend/agents/agent-a/skills/detect":
			_, _ = writer.Write([]byte(`{"ok":true,"agent_id":"agent-a","requested":1,"detected":1,"missing_skill_unique_ids":[],"results":[{"skill_unique_id":"skill-1","name":"research","detect_result":{"object_id":"skill-1","name":"research","label":"benign","risk_level":"low","capabilities":[],"risk_labels":[],"policy_targets":[],"suggested_plugins":[]}}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer guardServer.Close()
	guardClient, err := guard.NewWithOperationClient(guardServer.URL, guardSecret, "v2.1", guardServer.Client(), guardServer.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	const adminToken = "admin-token-with-enough-entropy"
	handler := New(ServerConfig{
		Sessions: auth.New(adminToken, auth.Options{TTL: time.Hour}), Trust: trust.New(t.Context(), guardClient, time.Second),
		Logger: slog.New(slog.DiscardHandler), AuthEnabled: true,
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	login, err := server.Client().Post(server.URL+"/api/v1/auth/session", "application/json", strings.NewReader(`{"token":"`+adminToken+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	cookie := login.Cookies()[0]
	csrf := login.Header.Get("X-CSRF-Token")
	_ = login.Body.Close()

	agentsRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/trust/agents", nil)
	agentsRequest.AddCookie(cookie)
	agentsResponse, err := server.Client().Do(agentsRequest)
	if err != nil {
		t.Fatal(err)
	}
	var agents model.ResourcePageEnvelope[model.TrustAgent]
	if err := json.NewDecoder(agentsResponse.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	_ = agentsResponse.Body.Close()
	if agents.Data.Total != 1 || agents.Data.Items[0].UpstreamID != "agent-a" {
		t.Fatalf("unexpected agents: %#v", agents)
	}
	agentID := agents.Data.Items[0].ID

	resourcesRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/trust/resources", nil)
	resourcesRequest.AddCookie(cookie)
	resourcesResponse, err := server.Client().Do(resourcesRequest)
	if err != nil {
		t.Fatal(err)
	}
	var resources model.ResourcePageEnvelope[model.TrustResource]
	if err := json.NewDecoder(resourcesResponse.Body).Decode(&resources); err != nil {
		t.Fatal(err)
	}
	_ = resourcesResponse.Body.Close()
	if resources.Data.Total != 2 {
		t.Fatalf("unexpected resources: %#v", resources)
	}
	toolID := resources.Data.Items[0].ID
	skillID := resources.Data.Items[1].ID

	labelURL := server.URL + "/api/v1/trust/agents/" + agentID + "/tools/" + toolID + "/labels"
	withoutCSRF, _ := http.NewRequest(http.MethodPatch, labelURL, strings.NewReader(`{"boundary":"external"}`))
	withoutCSRF.AddCookie(cookie)
	withoutCSRFResponse, err := server.Client().Do(withoutCSRF)
	if err != nil {
		t.Fatal(err)
	}
	_ = withoutCSRFResponse.Body.Close()
	if withoutCSRFResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("label update without CSRF status = %d", withoutCSRFResponse.StatusCode)
	}

	withCSRF, _ := http.NewRequest(http.MethodPatch, labelURL, strings.NewReader(`{"boundary":"external"}`))
	withCSRF.AddCookie(cookie)
	withCSRF.Header.Set("Content-Type", "application/json")
	withCSRF.Header.Set("X-CSRF-Token", csrf)
	labelResponse, err := server.Client().Do(withCSRF)
	if err != nil {
		t.Fatal(err)
	}
	var updated model.ResourceEnvelope[model.TrustResource]
	if err := json.NewDecoder(labelResponse.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	_ = labelResponse.Body.Close()
	if labelResponse.StatusCode != http.StatusOK || updated.Data.Labels == nil || updated.Data.Labels.Boundary != "server-confirmed" {
		t.Fatalf("unexpected label response: status=%d data=%#v", labelResponse.StatusCode, updated.Data)
	}

	scanRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/trust/agents/"+agentID+"/skills/detect", strings.NewReader(`{"resourceIds":["`+skillID+`"]}`))
	scanRequest.AddCookie(cookie)
	scanRequest.Header.Set("Content-Type", "application/json")
	scanRequest.Header.Set("X-CSRF-Token", csrf)
	scanResponse, err := server.Client().Do(scanRequest)
	if err != nil {
		t.Fatal(err)
	}
	var scan model.ResourceEnvelope[model.TrustScanJob]
	if err := json.NewDecoder(scanResponse.Body).Decode(&scan); err != nil {
		t.Fatal(err)
	}
	_ = scanResponse.Body.Close()
	if scanResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("scan status = %d", scanResponse.StatusCode)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pollRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/trust/scans/"+scan.Data.ID, nil)
		pollRequest.AddCookie(cookie)
		pollResponse, pollErr := server.Client().Do(pollRequest)
		if pollErr != nil {
			t.Fatal(pollErr)
		}
		var current model.ResourceEnvelope[model.TrustScanJob]
		if err := json.NewDecoder(pollResponse.Body).Decode(&current); err != nil {
			t.Fatal(err)
		}
		_ = pollResponse.Body.Close()
		if current.Data.Status == "succeeded" {
			if len(current.Data.Results) != 1 || current.Data.Results[0].Label != "benign" {
				t.Fatalf("unexpected scan result: %#v", current.Data)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("scan did not complete")
}
