package trace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

const (
	testTraceID = "11111111111111111111111111111111"
	testRootID  = "2222222222222222"
	testChildID = "3333333333333333"
)

type traceStoreStub struct {
	page           storage.Page[telemetry.Summary]
	listErr        error
	observedFilter storage.TraceFilter
	detail         storage.TraceDetail
	detailErr      error
	spanDetail     storage.TraceSpanDetail
	spanDetailErr  error
	listCalls      int
	detailCalls    int
	spanCalls      int
}

func (store *traceStoreStub) ListTraceSummaries(_ context.Context, filter storage.TraceFilter) (storage.Page[telemetry.Summary], error) {
	store.listCalls++
	store.observedFilter = filter
	return store.page, store.listErr
}

func (store *traceStoreStub) GetTraceDetail(context.Context, string, storage.TraceGraphLimits) (storage.TraceDetail, error) {
	store.detailCalls++
	return store.detail, store.detailErr
}

func (store *traceStoreStub) GetTraceSpanDetail(context.Context, string, string) (storage.TraceSpanDetail, error) {
	store.spanCalls++
	return store.spanDetail, store.spanDetailErr
}

func TestListNormalizesAndForwardsStableFilters(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 1, 2, 3, 0, time.UTC)
	hasErrors := false
	store := &traceStoreStub{page: storage.Page[telemetry.Summary]{
		Items: []telemetry.Summary{{TraceID: testTraceID, Status: "succeeded", Completeness: "verified"}}, Total: 1,
	}}
	service := New(store)
	service.now = func() time.Time { return now }
	envelope, err := service.List(t.Context(), Filter{
		Limit: 20, Status: " SUCCEEDED ", Completeness: " Verified ", AgentID: " agent-a ",
		HasError: &hasErrors, Query: " task-a ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Items) != 1 || envelope.Data.Total != 1 || !envelope.Meta.FetchedAt.Equal(now) {
		t.Fatalf("unexpected Trace page: %#v", envelope)
	}
	if store.observedFilter.Status != "succeeded" || store.observedFilter.Completeness != "verified" ||
		store.observedFilter.AgentID != "agent-a" || store.observedFilter.Query != "task-a" ||
		store.observedFilter.HasError == nil || *store.observedFilter.HasError {
		t.Fatalf("filter was not normalized and forwarded: %#v", store.observedFilter)
	}
}

func TestListRejectsInvalidQueryBeforeStorageAndMapsCursor(t *testing.T) {
	t.Parallel()
	store := &traceStoreStub{}
	service := New(store)
	if _, err := service.List(t.Context(), Filter{Limit: 25, Status: "invented"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid status error = %v", err)
	}
	if store.listCalls != 0 {
		t.Fatal("invalid filter reached storage")
	}
	store.listErr = storage.ErrInvalidTraceCursor
	if _, err := service.List(t.Context(), Filter{Limit: 25, Cursor: "bad"}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestDetailReturnsRootRelationshipsLinksAndVisibleCoverageWithoutRawFields(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	ended := started.Add(time.Second)
	root := telemetry.Span{
		TraceID: testTraceID, SpanID: testRootID, Name: "Task", OpenInferenceKind: "AGENT",
		StartedAt: started, EndedAt: &ended, StatusCode: telemetry.StatusOK, AgentID: "root-agent",
		SessionID: "session-a", TaskID: "task-a", ContentState: telemetry.ContentStateCaptured,
		InstrumentationScope: "agentshark.sdk", Attributes: map[string]any{"private": "root-secret"},
	}
	child := telemetry.Span{
		TraceID: testTraceID, SpanID: testChildID, ParentSpanID: testRootID, Name: "LLM",
		OpenInferenceKind: "LLM", StartedAt: started.Add(time.Millisecond), EndedAt: &ended,
		StatusCode: telemetry.StatusOK, AgentID: "root-agent", Provider: "openai", Model: "model-a",
		PeerAgentID: "peer-b", ContentState: telemetry.ContentStateRedacted,
		InstrumentationScope: "openinference.langchain", Events: []telemetry.Event{{Name: "private-event"}},
	}
	store := &traceStoreStub{
		detail: storage.TraceDetail{
			Summary:  telemetry.Summary{TraceID: testTraceID, RootSpanID: testRootID, Status: "succeeded", Completeness: "verified"},
			RootSpan: &root,
			Graph: storage.TraceGraph{
				Spans: []telemetry.Span{child}, Links: []telemetry.Link{{
					TraceID: testTraceID, SpanID: testChildID, LinkedTraceID: testTraceID,
					LinkedSpanID: testRootID, Attributes: map[string]any{"link.type": "async"},
				}},
				TotalSpans: 2, TotalLinks: 1, SpansTruncated: true,
			},
		},
	}
	envelope, err := New(store).Detail(t.Context(), stringsUpper(testTraceID))
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Data.RootSpan == nil || envelope.Data.RootSpan.SpanID != testRootID {
		t.Fatalf("root Span was not recovered outside the bounded graph: %#v", envelope.Data.RootSpan)
	}
	if store.detailCalls != 1 {
		t.Fatalf("atomic Trace detail calls = %d", store.detailCalls)
	}
	if len(envelope.Data.Spans) != 2 || envelope.Data.Spans[0].SpanID != testRootID ||
		envelope.Data.Spans[1].ParentSpanID != testRootID ||
		len(envelope.Data.Links) != 1 || envelope.Data.Links[0].LinkedSpanID != testRootID {
		t.Fatalf("deterministic relationships were lost: %#v", envelope.Data)
	}
	coverage := envelope.Data.Coverage
	if coverage.Source != coverageSource || len(coverage.AgentIDs) != 1 || coverage.AgentIDs[0] != "root-agent" ||
		len(coverage.PeerAgentIDs) != 1 || coverage.PeerAgentIDs[0] != "peer-b" ||
		len(coverage.Providers) != 1 || coverage.Providers[0] != "openai" {
		t.Fatalf("unexpected visible coverage: %#v", coverage)
	}
}

func TestSpanDetailReturnsRawFieldsEventsAndOnlyStoredPayloads(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	store := &traceStoreStub{
		spanDetail: storage.TraceSpanDetail{
			Span: telemetry.Span{
				TraceID: testTraceID, SpanID: testChildID, Name: "tool", StartedAt: now,
				StatusCode: telemetry.StatusError, StatusMessage: "tool failed", ContentState: telemetry.ContentStateTruncated,
				Attributes: map[string]any{"tool.name": "mail.send"}, Resource: map[string]any{"service.name": "agent"},
				Events: []telemetry.Event{{Name: "exception", Time: now, Attributes: map[string]any{"exception.type": "ValueError"}}},
			},
			Payloads: []telemetry.Payload{{
				TraceID: testTraceID, SpanID: testChildID, Kind: "tool.arguments", ContentType: "application/json",
				Encoding: "identity", PayloadJSON: []byte(`{"recipient":"user@example.test"}`),
				RedactionState: telemetry.ContentStateCaptured, SizeBytes: 33, CreatedAt: now,
			}},
		},
	}
	envelope, err := New(store).SpanDetail(t.Context(), testTraceID, stringsUpper(testChildID))
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Data.StatusMessage != "tool failed" || envelope.Data.Attributes["tool.name"] != "mail.send" ||
		len(envelope.Data.Events) != 1 || len(envelope.Data.Payloads) != 1 ||
		string(envelope.Data.Payloads[0].PayloadJSON) != `{"recipient":"user@example.test"}` {
		t.Fatalf("single-Span detail is incomplete: %#v", envelope.Data)
	}
	if store.spanCalls != 1 {
		t.Fatalf("atomic Span detail calls = %d", store.spanCalls)
	}
}

func TestSpanDetailMarksPreviouslyCapturedContentExpiredWhenNoPayloadRemains(t *testing.T) {
	t.Parallel()
	store := &traceStoreStub{spanDetail: storage.TraceSpanDetail{Span: telemetry.Span{
		TraceID: testTraceID, SpanID: testChildID, Name: "llm", StartedAt: time.Now().UTC(),
		StatusCode: telemetry.StatusOK, ContentState: telemetry.ContentStateCaptured,
	}}}
	envelope, err := New(store).SpanDetail(t.Context(), testTraceID, testChildID)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Span.ContentState != telemetry.ContentStateExpired || envelope.Data.Payloads == nil || len(envelope.Data.Payloads) != 0 {
		t.Fatalf("expired content remained ambiguous: %#v", envelope.Data)
	}
}

func stringsUpper(value string) string {
	result := []byte(value)
	for index, character := range result {
		if character >= 'a' && character <= 'f' {
			result[index] = character - ('a' - 'A')
		}
	}
	return string(result)
}
