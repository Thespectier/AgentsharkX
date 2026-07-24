package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/audit"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/protect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/trust"
)

type apiAuditGateway struct {
	feed      model.AuditFeed
	analytics model.GatewayAnalytics
}

func (fake apiAuditGateway) TrafficWindow(context.Context, int, model.TrendWindow) (model.AuditFeed, error) {
	return fake.feed, nil
}
func (fake apiAuditGateway) AnalyticsWindow(context.Context, model.TrendWindow) (model.GatewayAnalytics, error) {
	return fake.analytics, nil
}

type apiAuditGuard struct {
	traffic  model.AuditFeed
	audit    model.AuditFeed
	sessions []model.AuditSession
}

func (fake apiAuditGuard) Traffic(context.Context, int) (model.AuditFeed, error) {
	return fake.traffic, nil
}
func (fake apiAuditGuard) Audit(context.Context, int) (model.AuditFeed, error) {
	return fake.audit, nil
}
func (fake apiAuditGuard) AuditSessions(context.Context) ([]model.AuditSession, error) {
	return fake.sessions, nil
}

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

func TestProtectRulesAndApprovalsFlowThroughBFF(t *testing.T) {
	t.Parallel()

	var auditLog bytes.Buffer
	const secretSource = "RULE review_email\nACTION HUMAN_CHECK\nSECRET never-log-rule-source"
	const secretNote = "reviewed never-log-operator-note"
	gatewayServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/api/config" {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, `{"binds":[{"port":8080,"listeners":[{"name":"http","protocol":"HTTP","routes":[{"name":"api","policies":{"cors":{"allowOrigins":["never-log-policy-body"]}},"backends":[{"host":"localhost:9000"}]}]}]}]}`)
	}))
	defer gatewayServer.Close()

	guardServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/v1/backend/sessions":
			_, _ = io.WriteString(writer, `{"sessions":[{"session_id":"session-a","agent_id":"agent-a","user_id":"user-a","last_seen":1784688000}]}`)
		case request.URL.Path == "/v1/backend/tools" || request.URL.Path == "/v1/backend/skills" || request.URL.Path == "/v1/backend/mcps":
			_, _ = io.WriteString(writer, `[]`)
		case request.URL.Path == "/v1/backend/rules" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `[{"id":"existing","name":"existing","rule_id":"existing","status":"published","tool_pattern":"mail.send","action":"HUMAN_CHECK","severity":"high","category":"boundary","reason":"review","pack_id":"agent::agent-a","user_managed":true}]`)
		case request.URL.Path == "/v1/backend/rules/check":
			_, _ = io.WriteString(writer, `{"ok":true,"rule_count":1,"errors":[],"warnings":[],"hints":[]}`)
		case request.URL.Path == "/v1/backend/agents/agent-a/rules" && request.Method == http.MethodPost:
			_, _ = io.WriteString(writer, `{"ok":true,"agent_id":"agent-a","pack_id":"agent::agent-a","rule_id":"review_email","created":true}`)
		case request.URL.Path == "/v1/backend/agents/agent-a/rules/existing" && request.Method == http.MethodDelete:
			_, _ = io.WriteString(writer, `{"ok":true,"agent_id":"agent-a","pack_id":"agent::agent-a","rule_id":"existing"}`)
		case request.URL.Path == "/v1/backend/agents/agent-a/plugins/config":
			_, _ = io.WriteString(writer, `{"agent_id":"agent-a","plugin_config":{"phases":{"tool_before":{"client":["tool_invoke"],"server":[]}}},"config_source":"server_default"}`)
		case request.URL.Path == "/v1/backend/agents/agent-a/plugins/available":
			_, _ = io.WriteString(writer, `{"agent_id":"agent-a","local_plugins":[{"name":"tool_invoke","phases":["tool_before"]}],"remote_plugins":[]}`)
		case request.URL.Path == "/v1/backend/approvals" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `[{"ticket_id":"ticket-ok","created_ms":1784688000000,"status":"pending","event":{"event_id":"event-ok","event_type":"tool_invoke","principal":{"agent_id":"agent-a"},"tool_call":{"tool_name":"mail.send","args":{"body":"never-log-approval-body"}}},"decision":{"action":"human_check","risk_score":0.8,"matched_rules":["existing"],"reason":"review"}},{"ticket_id":"ticket-deny","created_ms":1784688000001,"status":"pending","event":{"event_id":"event-deny","event_type":"tool_invoke","principal":{"agent_id":"agent-a","session_id":"session-a","user_id":"user-a"},"tool_call":{"tool_name":"database.write","args":{"query":"never-log-approval-query"}}},"decision":{"action":"human_check","risk_score":0.9,"matched_rules":["existing"],"reason":"never-log-approval-reason"}},{"ticket_id":"ticket-gone","created_ms":1784688000002,"status":"pending","event":{"event_id":"event-gone","event_type":"tool_invoke","principal":{"agent_id":"agent-a"},"tool_call":{"tool_name":"shell.exec"}},"decision":{"action":"human_check","risk_score":0.9,"matched_rules":["existing"],"reason":"review"}}]`)
		case request.URL.Path == "/v1/backend/approvals/ticket-ok/approve":
			_, _ = io.WriteString(writer, `{"ok":true}`)
		case request.URL.Path == "/v1/backend/approvals/ticket-deny/deny":
			_, _ = io.WriteString(writer, `{"ok":true}`)
		case request.URL.Path == "/v1/backend/approvals/ticket-gone/deny":
			http.NotFound(writer, request)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer guardServer.Close()

	gatewayClient, err := gateway.New(gatewayServer.URL, gatewayServer.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	guardClient, err := guard.New(guardServer.URL, "guard-secret", guardServer.Client(), 2)
	if err != nil {
		t.Fatal(err)
	}
	auditService := audit.New(apiAuditGateway{}, apiAuditGuard{}, nil)
	server := httptest.NewServer(New(ServerConfig{
		Protect: protect.New(gatewayClient, guardClient, model.ConsoleLinks{RawConfig: "http://gateway.invalid/ui/config"}, auditService),
		Logger:  slog.New(slog.NewTextHandler(&auditLog, nil)), AuthEnabled: false,
	}))
	defer server.Close()

	var snapshot model.ProtectSnapshotEnvelope
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/protect/policies", "", http.StatusOK, &snapshot)
	if len(snapshot.Data.GatewayPolicies) != 1 || len(snapshot.Data.RuntimeRules) != 1 || len(snapshot.Data.Plugins) != 4 {
		t.Fatalf("unexpected protect snapshot: %#v", snapshot)
	}
	if snapshot.Meta.Partial {
		t.Fatalf("successful fake upstream snapshot became partial: %#v", snapshot.Meta)
	}
	agentID := snapshot.Data.Plugins[0].AgentID
	ruleID := snapshot.Data.RuntimeRules[0].ID

	var check model.ResourceEnvelope[model.RuntimeRuleCheck]
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/runtime-rules/check", `{"source":"`+strings.ReplaceAll(secretSource, "\n", `\n`)+`"}`, http.StatusOK, &check)
	if !check.Data.Publishable || check.Data.CheckToken == "" || check.Data.RequestID == "" {
		t.Fatalf("unexpected check receipt: %#v", check.Data)
	}
	publishBody, _ := json.Marshal(model.RuntimeRulePublishRequest{Source: secretSource, CheckToken: check.Data.CheckToken, Note: secretNote, Confirmed: true})
	var publish model.ProtectMutationEnvelope
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/agents/"+agentID+"/runtime-rules", string(publishBody), http.StatusCreated, &publish)
	if publish.Data.RequestID == "" || publish.Data.Operation != "publish-runtime-rule" {
		t.Fatalf("unexpected publish receipt: %#v", publish)
	}
	var deleted model.ProtectMutationEnvelope
	protectJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/protect/agents/"+agentID+"/runtime-rules/"+ruleID, `{"note":"retired","confirmed":true}`, http.StatusOK, &deleted)

	var approvals model.ResourcePageEnvelope[model.Approval]
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/protect/approvals", "", http.StatusOK, &approvals)
	if approvals.Data.Total != 3 {
		t.Fatalf("unexpected approvals: %#v", approvals)
	}
	byTool := map[string]string{}
	for _, approval := range approvals.Data.Items {
		byTool[approval.Tool] = approval.ID
	}
	var approved model.ProtectMutationEnvelope
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/approvals/"+byTool["mail.send"]+"/approve", `{"note":"`+secretNote+`","confirmed":true}`, http.StatusOK, &approved)
	var denied model.ProtectMutationEnvelope
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/approvals/"+byTool["database.write"]+"/deny", `{"note":"never-log-denial-note","confirmed":true}`, http.StatusOK, &denied)
	var missing model.ErrorEnvelope
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/approvals/"+byTool["shell.exec"]+"/deny", `{"note":"reviewed","confirmed":true}`, http.StatusNotFound, &missing)
	if missing.Error.Code != "NOT_FOUND" || missing.Error.RequestID == "" {
		t.Fatalf("unexpected missing ticket response: %#v", missing)
	}
	auditSnapshot := auditService.Snapshot()
	if len(auditSnapshot.Data.Events) != 2 || auditSnapshot.Data.Events[0].Decision != "DENY" ||
		auditSnapshot.Data.Events[0].Target.Tool != "database.write" {
		t.Fatalf("confirmed approval outcomes were not recorded in Audit: %#v", auditSnapshot.Data.Events)
	}
	denialDetail, ok := auditService.Find(model.SourceAgentGuard, auditSnapshot.Data.Events[0].ID)
	if !ok {
		t.Fatal("confirmed denial detail was not retained")
	}
	denialJSON, _ := json.Marshal(denialDetail)
	for _, forbidden := range []string{"never-log-approval-query", "never-log-approval-reason", "never-log-denial-note"} {
		if strings.Contains(string(denialJSON), forbidden) {
			t.Fatalf("approval audit detail leaked %q: %s", forbidden, denialJSON)
		}
	}

	logs := auditLog.String()
	for _, forbidden := range []string{secretSource, "never-log-rule-source", secretNote, "never-log-operator-note", "never-log-approval-body", "never-log-approval-query", "never-log-approval-reason", "never-log-denial-note", "never-log-policy-body"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("audit log leaked %q: %s", forbidden, logs)
		}
	}
	if !strings.Contains(logs, "protect operation completed") || !strings.Contains(logs, "request_id=") || !strings.Contains(logs, "note_present=true") {
		t.Fatalf("missing safe protect audit fields: %s", logs)
	}
}

func TestApprovalTimeoutIsNotRetriedAndManualRetrySucceeds(t *testing.T) {
	t.Parallel()
	var mutationCalls int
	var mu sync.Mutex
	guardServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/backend/approvals" {
			_, _ = io.WriteString(writer, `[{"ticket_id":"ticket-retry","created_ms":1784688000000,"status":"pending","event":{"event_id":"event-retry","event_type":"tool_invoke","principal":{"agent_id":"agent-a"},"tool_call":{"tool_name":"mail.send"}},"decision":{"action":"human_check","risk_score":0.5,"matched_rules":[],"reason":"review"}}]`)
			return
		}
		if request.URL.Path == "/v1/backend/approvals/ticket-retry/approve" {
			mu.Lock()
			mutationCalls++
			call := mutationCalls
			mu.Unlock()
			if call == 1 {
				time.Sleep(60 * time.Millisecond)
			}
			_, _ = io.WriteString(writer, `{"ok":true}`)
			return
		}
		http.NotFound(writer, request)
	}))
	defer guardServer.Close()
	readClient := guardServer.Client()
	operationClient := &http.Client{Timeout: 10 * time.Millisecond}
	guardClient, err := guard.NewWithOperationClient(guardServer.URL, "guard-secret", "v2.1", readClient, operationClient, 3)
	if err != nil {
		t.Fatal(err)
	}
	service := protect.New(fakeProtectGateway{}, guardClient, model.ConsoleLinks{})
	server := httptest.NewServer(New(ServerConfig{Protect: service, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false}))
	defer server.Close()
	var approvals model.ResourcePageEnvelope[model.Approval]
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/protect/approvals", "", http.StatusOK, &approvals)
	ticketID := approvals.Data.Items[0].ID
	var timeout model.ErrorEnvelope
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/approvals/"+ticketID+"/approve", `{"note":"reviewed","confirmed":true}`, http.StatusServiceUnavailable, &timeout)
	if !timeout.Error.Retryable {
		t.Fatalf("timeout should invite an explicit retry: %#v", timeout)
	}
	mu.Lock()
	firstCalls := mutationCalls
	mu.Unlock()
	if firstCalls != 1 {
		t.Fatalf("timed out mutation was retried automatically: %d", firstCalls)
	}
	var retry model.ProtectMutationEnvelope
	protectJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protect/approvals/"+ticketID+"/approve", `{"note":"reviewed","confirmed":true}`, http.StatusOK, &retry)
	mu.Lock()
	defer mu.Unlock()
	if mutationCalls != 2 || retry.Data.RequestID == "" {
		t.Fatalf("manual retry did not succeed: calls=%d receipt=%#v", mutationCalls, retry)
	}
}

type fakeProtectGateway struct{}

func (fakeProtectGateway) Snapshot(context.Context) (model.GatewaySnapshot, error) {
	return model.GatewaySnapshot{FetchedAt: time.Now().UTC()}, nil
}

func protectJSON(t *testing.T, client *http.Client, method, endpoint, body string, wantStatus int, destination any) {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s status=%d want=%d body=%s", method, endpoint, response.StatusCode, wantStatus, payload)
	}
	if destination != nil {
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			t.Fatal(err)
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

func TestAuditRoutesExposeBoundedListsAndRedactedDetail(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	event := model.UnifiedEvent{
		ID: "guard:event-1", Timestamp: now, Source: model.SourceAgentGuard, Kind: "audit", Severity: "high",
		Subject: &model.EventSubject{AgentID: "agent-a", SessionID: "session-a"}, Decision: "DENY",
		Summary: "tool invoke was denied", RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "event-1"},
		Raw: map[string]any{"eventId": "event-1", "redacted": true},
	}
	hub := stream.NewHub()
	auditService := audit.New(
		apiAuditGateway{
			feed:      model.AuditFeed{Status: "unavailable", Reason: "request-log storage is not configured", Events: []model.UnifiedEvent{}},
			analytics: model.GatewayAnalytics{Status: "unavailable", Reason: "analytics storage is not configured", Buckets: []model.AnalyticsBucket{}},
		},
		apiAuditGuard{
			traffic: model.AuditFeed{Status: "available", Traffic: []model.AuditTrafficRecord{{Timestamp: now, Action: "DENY", LatencyMS: 4}}},
			audit:   model.AuditFeed{Status: "available", Events: []model.UnifiedEvent{event}},
			sessions: []model.AuditSession{{
				ID: "session-resource", UpstreamID: "session-a", AgentID: "agent-resource", AgentUpstreamID: "agent-a",
				Source: model.SourceAgentGuard, Status: "unknown", RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "session-a"},
			}},
		}, hub,
	)
	auditService.Refresh(t.Context())
	server := httptest.NewServer(New(ServerConfig{Audit: auditService, Stream: hub, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false}))
	defer server.Close()

	var analytics model.AuditEnvelope
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/audit/analytics", "", http.StatusOK, &analytics)
	if !analytics.Meta.Partial || len(analytics.Data.Events) != 1 || analytics.Data.Events[0].Raw != nil {
		t.Fatalf("unexpected audit analytics: %#v", analytics)
	}
	var events model.EventsEnvelope
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/audit/events?source=agentguard&limit=1", "", http.StatusOK, &events)
	if events.Data.Total != 1 || events.Data.Items[0].ID != event.ID {
		t.Fatalf("unexpected audit page: %#v", events)
	}
	var detail model.ResourceEnvelope[model.UnifiedEvent]
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/audit/events/agentguard/guard:event-1", "", http.StatusOK, &detail)
	if detail.Data.Raw["redacted"] != true {
		t.Fatalf("redacted detail missing: %#v", detail)
	}
	var sessions model.ResourceEnvelope[[]model.AuditSession]
	protectJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/audit/sessions", "", http.StatusOK, &sessions)
	if len(sessions.Data) != 1 || sessions.Data[0].Events != 1 || sessions.Data[0].Denies != 1 {
		t.Fatalf("unexpected audit sessions: %#v", sessions)
	}
}

func TestStreamResumesAfterLastSequenceWithoutDuplicateReplay(t *testing.T) {
	t.Parallel()
	hub := stream.NewHubWithCapacity(10)
	for index, id := range []string{"gateway:first", "guard:second", "gateway:third"} {
		sourceValue := model.SourceAgentGateway
		kind := "traffic"
		if index == 1 {
			sourceValue = model.SourceAgentGuard
			kind = "audit"
		}
		hub.Publish(model.UnifiedEvent{
			ID: id, Timestamp: time.Now().UTC(), Source: sourceValue, Kind: kind, Severity: "info",
			Summary: id, RawRef: model.RawRef{Source: sourceValue, ID: id},
		})
	}
	server := httptest.NewServer(New(ServerConfig{Stream: hub, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/stream", nil)
	request.Header.Set("Last-Event-ID", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	blocks := make([]string, 0, 2)
	var block []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			blocks = append(blocks, strings.Join(block, "\n"))
			block = nil
			if len(blocks) == 2 {
				break
			}
			continue
		}
		block = append(block, line)
	}
	joined := strings.Join(blocks, "\n")
	if strings.Contains(joined, "gateway:first") || !strings.Contains(blocks[0], "id: 2") || !strings.Contains(blocks[1], "id: 3") {
		t.Fatalf("unexpected resumed stream: %#v", blocks)
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
