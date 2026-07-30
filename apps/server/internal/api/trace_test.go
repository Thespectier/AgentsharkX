package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage/memory"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	tracequery "github.com/Thespectier/AgentsharkX/apps/server/internal/trace"
)

const (
	apiTraceID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	apiRootID  = "bbbbbbbbbbbbbbbb"
	apiChildID = "cccccccccccccccc"
)

func TestTraceEndpointsKeepPayloadsOutOfListAndGraphButReturnAuthenticatedSpanDetail(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	store := memory.New(memory.Options{Now: func() time.Time { return now }, PayloadRetention: time.Hour})
	ended := now.Add(2 * time.Second)
	root := telemetry.Span{
		TraceID: apiTraceID, SpanID: apiRootID, Name: "Task", OpenInferenceKind: "AGENT",
		StartedAt: now.Add(-time.Minute), EndedAt: &ended, StatusCode: telemetry.StatusOK,
		AgentID: "agent-a", SessionID: "session-a", TaskID: "task-a", ContentState: telemetry.ContentStateCaptured,
		Attributes: map[string]any{telemetry.AttributeTaskRoot: true, "raw.secret": "root-attribute-secret"},
		Resource:   map[string]any{"service.name": "example-agent"},
	}
	child := telemetry.Span{
		TraceID: apiTraceID, SpanID: apiChildID, ParentSpanID: apiRootID, Name: "ChatModel.invoke",
		OpenInferenceKind: "LLM", StartedAt: now.Add(-30 * time.Second), EndedAt: &ended,
		StatusCode: telemetry.StatusError, StatusMessage: "span-status-secret", AgentID: "agent-a",
		SessionID: "session-a", TaskID: "task-a", Provider: "provider-a", Model: "model-a",
		Countable: true, ContentState: telemetry.ContentStateCaptured,
		Attributes: map[string]any{"raw.secret": "span-attribute-secret"}, Resource: map[string]any{"service.name": "example-agent"},
		Events: []telemetry.Event{{Name: "span-event-secret", Time: now, Attributes: map[string]any{"event.secret": "event-value-secret"}}},
	}
	expiresAt := now.Add(time.Hour)
	_, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{
		Spans: []telemetry.Span{child, root},
		Links: []telemetry.Link{{
			TraceID: apiTraceID, SpanID: apiChildID, LinkedTraceID: apiTraceID,
			LinkedSpanID: apiRootID, Attributes: map[string]any{"link.secret": "link-attribute-secret"},
		}},
		Payloads: []telemetry.Payload{{
			TraceID: apiTraceID, SpanID: apiChildID, Kind: "input", ContentType: "application/json", Encoding: "identity",
			PayloadJSON: []byte(`{"prompt":"retained-prompt-secret"}`), RedactionState: telemetry.ContentStateCaptured,
			SizeBytes: 35, ExpiresAt: &expiresAt, CreatedAt: now,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(ServerConfig{
		Traces: tracequery.New(store), Logger: slog.New(slog.DiscardHandler), AuthEnabled: false,
	}))
	defer server.Close()

	listURL := server.URL + "/api/v1/audit/traces?status=succeeded&completeness=verified&agent_id=agent-a&session_id=session-a&task_id=task-a&has_error=true&has_a2a=false&started_after=2026-07-30T00:00:00Z&started_before=2026-07-31T00:00:00Z&query=task-a"
	listBody := getTraceBody(t, server.Client(), listURL, http.StatusOK)
	assertTraceSecretsAbsent(t, listBody)
	var list model.TraceListEnvelope
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatal(err)
	}
	if list.Data.Total != 1 || len(list.Data.Items) != 1 || list.Data.Items[0].ErrorCount != 1 {
		t.Fatalf("unexpected filtered Trace list: %#v", list)
	}

	detailBody := getTraceBody(t, server.Client(), server.URL+"/api/v1/audit/traces/"+apiTraceID, http.StatusOK)
	assertTraceSecretsAbsent(t, detailBody)
	var detail model.TraceDetailEnvelope
	if err := json.Unmarshal(detailBody, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Data.RootSpan == nil || detail.Data.RootSpan.SpanID != apiRootID || len(detail.Data.Spans) != 2 ||
		detail.Data.Spans[1].ParentSpanID != apiRootID || len(detail.Data.Links) != 1 ||
		detail.Data.Coverage.Source != "agentshark-collector" {
		t.Fatalf("Trace graph relationships are incomplete: %#v", detail.Data)
	}

	spanBody := getTraceBody(t, server.Client(), server.URL+"/api/v1/audit/traces/"+apiTraceID+"/spans/"+apiChildID, http.StatusOK)
	for _, expected := range []string{"retained-prompt-secret", "span-attribute-secret", "span-event-secret", "event-value-secret", "span-status-secret"} {
		if !strings.Contains(string(spanBody), expected) {
			t.Fatalf("single-Span detail omitted %q: %s", expected, spanBody)
		}
	}
	var spanDetail model.TraceSpanDetailEnvelope
	if err := json.Unmarshal(spanBody, &spanDetail); err != nil {
		t.Fatal(err)
	}
	if len(spanDetail.Data.Payloads) != 1 || spanDetail.Data.Span.ContentState != telemetry.ContentStateCaptured {
		t.Fatalf("unexpected Span payload detail: %#v", spanDetail.Data)
	}
}

func TestTraceEndpointsValidateQueriesNotFoundAndAuthentication(t *testing.T) {
	t.Parallel()
	store := memory.New(memory.Options{})
	unauthenticated := httptest.NewServer(New(ServerConfig{
		Sessions: auth.New("admin-token-with-enough-entropy", auth.Options{TTL: time.Hour}),
		Traces:   tracequery.New(store), Logger: slog.New(slog.DiscardHandler), AuthEnabled: true,
	}))
	defer unauthenticated.Close()
	getTraceBody(t, unauthenticated.Client(), unauthenticated.URL+"/api/v1/audit/traces", http.StatusUnauthorized)

	server := httptest.NewServer(New(ServerConfig{
		Traces: tracequery.New(store), Logger: slog.New(slog.DiscardHandler), AuthEnabled: false,
	}))
	defer server.Close()
	for _, path := range []string{
		"/api/v1/audit/traces?status=invalid",
		"/api/v1/audit/traces?has_error=1",
		"/api/v1/audit/traces?started_after=2026-08-01T00:00:00Z&started_before=2026-07-01T00:00:00Z",
		"/api/v1/audit/traces?limit=101",
	} {
		body := getTraceBody(t, server.Client(), server.URL+path, http.StatusBadRequest)
		var envelope model.ErrorEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Code != "INVALID_REQUEST" {
			t.Fatalf("unexpected invalid query response for %s: %s", path, body)
		}
	}
	badCursor := getTraceBody(t, server.Client(), server.URL+"/api/v1/audit/traces?cursor=not-a-cursor", http.StatusBadRequest)
	var cursorEnvelope model.ErrorEnvelope
	if err := json.Unmarshal(badCursor, &cursorEnvelope); err != nil || cursorEnvelope.Error.Code != "INVALID_CURSOR" {
		t.Fatalf("unexpected invalid cursor response: %s", badCursor)
	}
	missing := getTraceBody(t, server.Client(), server.URL+"/api/v1/audit/traces/11111111111111111111111111111111", http.StatusNotFound)
	var envelope model.ErrorEnvelope
	if err := json.Unmarshal(missing, &envelope); err != nil || envelope.Error.Code != "NOT_FOUND" {
		t.Fatalf("unexpected missing Trace response: %s", missing)
	}
}

func TestAuthenticatedTraceSSEPollsCollectorOutboxAndRemainsPayloadSafe(t *testing.T) {
	t.Parallel()
	const adminToken = "admin-token-with-enough-entropy"
	now := time.Date(2026, 7, 30, 3, 0, 0, 0, time.UTC)
	store := memory.New(memory.Options{Now: func() time.Time { return now }, PayloadRetention: time.Hour})
	server := httptest.NewServer(New(ServerConfig{
		Sessions: auth.New(adminToken, auth.Options{TTL: time.Hour}), Outbox: store,
		StreamReplayInterval: 10 * time.Millisecond,
		Logger:               slog.New(slog.DiscardHandler), AuthEnabled: true,
	}))
	defer server.Close()

	unauthorized, err := server.Client().Get(server.URL + "/api/v1/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Trace stream status = %d", unauthorized.StatusCode)
	}
	login, err := server.Client().Post(
		server.URL+"/api/v1/auth/session", "application/json", strings.NewReader(`{"token":"`+adminToken+`"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	cookies := login.Cookies()
	_ = login.Body.Close()
	if login.StatusCode != http.StatusNoContent || len(cookies) != 1 {
		t.Fatalf("login status=%d cookies=%d", login.StatusCode, len(cookies))
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/stream", nil)
	request.AddCookie(cookies[0])
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	ended := now.Add(time.Second)
	expiresAt := now.Add(time.Hour)
	_, err = store.WriteBatch(t.Context(), telemetry.TraceBatch{
		Spans: []telemetry.Span{{
			TraceID: apiTraceID, SpanID: apiRootID, Name: "Task", StartedAt: now, EndedAt: &ended,
			StatusCode: telemetry.StatusOK, AgentID: "agent-a", SessionID: "session-a", TaskID: "task-a",
			ContentState: telemetry.ContentStateCaptured,
			Attributes:   map[string]any{telemetry.AttributeTaskRoot: true, "private": "sse-attribute-secret"},
			Events:       []telemetry.Event{{Name: "sse-event-secret", Time: now}},
		}},
		Payloads: []telemetry.Payload{{
			TraceID: apiTraceID, SpanID: apiRootID, Kind: "task.goal", ContentType: "application/json", Encoding: "identity",
			PayloadJSON: []byte(`{"goal":"sse-payload-secret"}`), RedactionState: telemetry.ContentStateCaptured,
			SizeBytes: 29, ExpiresAt: &expiresAt, CreatedAt: now,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	block := readSSEBlock(t, response.Body)
	for _, expected := range []string{"event: trace", `"traceId":"` + apiTraceID + `"`} {
		if !strings.Contains(block, expected) {
			t.Fatalf("Trace SSE omitted %q: %s", expected, block)
		}
	}
	for _, forbidden := range []string{"sse-attribute-secret", "sse-event-secret", "sse-payload-secret", `"attributes"`, `"payloads"`} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("Trace SSE leaked %q: %s", forbidden, block)
		}
	}
}

func getTraceBody(t *testing.T, client *http.Client, url string, status int) []byte {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("GET %s status=%d want=%d body=%s", url, response.StatusCode, status, body)
	}
	return body
}

func assertTraceSecretsAbsent(t *testing.T, body []byte) {
	t.Helper()
	for _, forbidden := range []string{
		"retained-prompt-secret", "root-attribute-secret", "span-attribute-secret",
		"span-event-secret", "event-value-secret", "span-status-secret", "link-attribute-secret",
	} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("payload-free Trace response leaked %q: %s", forbidden, body)
		}
	}
}
