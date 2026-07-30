package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

const memoryTraceID = "11111111111111111111111111111111"

func TestTraceStoreHandlesOutOfOrderIdempotentAndTerminalUpdates(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	store := New(Options{Now: func() time.Time { return now }})
	child := memoryTraceSpan("2222222222222222", "LLM", true, now.Add(time.Second), now.Add(2*time.Second))
	input, output := int64(4), int64(6)
	child.InputTokens, child.OutputTokens = &input, &output
	first, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{child}})
	if err != nil || first.Inserted != 1 || first.Updated != 0 || first.Duplicates != 0 {
		t.Fatalf("first write = %#v, %v", first, err)
	}
	summary, err := store.GetTraceSummary(t.Context(), memoryTraceID)
	if err != nil || summary.Status != "unknown" || summary.Completeness != "partial" || summary.DurationMS != nil || summary.LLMCalls != 1 {
		t.Fatalf("child-first summary = %#v, %v", summary, err)
	}

	root := memoryTraceSpan("1111111111111111", "AGENT", false, now, time.Time{})
	root.AgentID, root.SessionID, root.TaskID = "agent", "session", "task"
	root.Attributes[telemetry.AttributeTaskRoot] = true
	root.Attributes["private.attribute"] = "attribute-secret"
	now = now.Add(3 * time.Second)
	running, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{root}})
	if err != nil || running.Inserted != 1 {
		t.Fatalf("running root = %#v, %v", running, err)
	}
	summary, _ = store.GetTraceSummary(t.Context(), memoryTraceID)
	if summary.Status != "running" || summary.Completeness != "partial" || summary.DurationMS != nil {
		t.Fatalf("running summary = %#v", summary)
	}

	endedAt := root.StartedAt.Add(5 * time.Second)
	root.EndedAt = &endedAt
	root.StatusCode = telemetry.StatusOK
	now = now.Add(time.Second)
	completed, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{root, child}})
	if err != nil || completed.Updated != 1 || completed.Duplicates != 1 {
		t.Fatalf("completion write = %#v, %v", completed, err)
	}
	summary, _ = store.GetTraceSummary(t.Context(), memoryTraceID)
	if summary.Status != "succeeded" || summary.Completeness != "verified" || summary.DurationMS == nil ||
		*summary.DurationMS != 5000 || summary.LLMCalls != 1 || summary.TotalTokens != 10 {
		t.Fatalf("completed summary = %#v", summary)
	}

	root.EndedAt = nil
	now = now.Add(time.Second)
	stale, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{root}})
	if err != nil || stale.Duplicates != 1 || stale.Updated != 0 {
		t.Fatalf("stale retry = %#v, %v", stale, err)
	}
	summary, _ = store.GetTraceSummary(t.Context(), memoryTraceID)
	if summary.Status != "succeeded" || summary.DurationMS == nil || *summary.DurationMS != 5000 {
		t.Fatalf("terminal state regressed: %#v", summary)
	}
}

func TestTraceStorePersistsLinksAndSeparatelyRetainedPayloads(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	store := New(Options{Now: func() time.Time { return now }})
	span := memoryTraceSpan("1111111111111111", "TOOL", true, now, now.Add(time.Second))
	span.Attributes["custom.attribute"] = "kept"
	expiresAt := now.Add(time.Hour)
	payloadDocument := json.RawMessage(`{"arguments":{"query":"private"}}`)
	batch := telemetry.TraceBatch{
		Spans: []telemetry.Span{span},
		Links: []telemetry.Link{{
			TraceID: memoryTraceID, SpanID: span.SpanID,
			LinkedTraceID: "33333333333333333333333333333333", LinkedSpanID: "4444444444444444",
			Attributes: map[string]any{"messaging.operation": "publish"},
		}},
		Payloads: []telemetry.Payload{{
			TraceID: memoryTraceID, SpanID: span.SpanID, Kind: "tool.arguments",
			ContentType: "application/json", Encoding: "identity", PayloadJSON: payloadDocument,
			RedactionState: telemetry.ContentStateCaptured, SizeBytes: int64(len(payloadDocument)),
			ExpiresAt: &expiresAt, CreatedAt: now,
		}},
	}
	result, err := store.WriteBatch(t.Context(), batch)
	if err != nil || result.Inserted != 1 {
		t.Fatalf("write = %#v, %v", result, err)
	}
	spans, err := store.GetTraceSpans(t.Context(), memoryTraceID)
	if err != nil || len(spans) != 1 || spans[0].Attributes["custom.attribute"] != "kept" {
		t.Fatalf("spans = %#v, %v", spans, err)
	}
	links, err := store.GetTraceLinks(t.Context(), memoryTraceID)
	if err != nil || len(links) != 1 || links[0].Attributes["messaging.operation"] != "publish" {
		t.Fatalf("links = %#v, %v", links, err)
	}
	payload, err := store.GetTracePayload(t.Context(), memoryTraceID, span.SpanID, "tool.arguments")
	if err != nil || string(payload.PayloadJSON) != string(payloadDocument) {
		t.Fatalf("payload = %#v, %v", payload, err)
	}

	now = expiresAt.Add(time.Second)
	if _, err := store.GetTracePayload(t.Context(), memoryTraceID, span.SpanID, "tool.arguments"); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired payload error = %v", err)
	}
	if err := store.Prune(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	spans, err = store.GetTraceSpans(t.Context(), memoryTraceID)
	if err != nil || len(spans) != 1 || spans[0].ContentState != telemetry.ContentStateExpired {
		t.Fatalf("payload prune removed metadata: %#v, %v", spans, err)
	}
	replayedExpiry := now.Add(time.Hour)
	batch.Payloads[0].CreatedAt = now
	batch.Payloads[0].ExpiresAt = &replayedExpiry
	if _, err := store.WriteBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTracePayload(t.Context(), memoryTraceID, span.SpanID, "tool.arguments"); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired payload was revived by a duplicate span: %v", err)
	}
	spans, err = store.GetTraceSpans(t.Context(), memoryTraceID)
	if err != nil || spans[0].ContentState != telemetry.ContentStateExpired {
		t.Fatalf("expired content state regressed after replay: %#v, %v", spans, err)
	}
}

func TestTraceStoreRejectsCompanionsWithoutSpanAtomically(t *testing.T) {
	store := New(Options{})
	span := memoryTraceSpan("1111111111111111", "LLM", true, time.Now().UTC(), time.Now().UTC().Add(time.Second))
	badLink := telemetry.Link{
		TraceID: memoryTraceID, SpanID: "9999999999999999",
		LinkedTraceID: "33333333333333333333333333333333", LinkedSpanID: "4444444444444444",
		Attributes: map[string]any{},
	}
	if _, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{span}, Links: []telemetry.Link{badLink}}); err == nil {
		t.Fatal("missing source span was accepted")
	}
	if _, err := store.GetTraceSpans(t.Context(), memoryTraceID); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("failed batch was partially written: %v", err)
	}
}

func TestTraceStorePrunesMetadataByConfiguredRetention(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	store := New(Options{TraceRetention: time.Hour, Now: func() time.Time { return now }})
	span := memoryTraceSpan("1111111111111111", "LLM", true, now, now.Add(time.Second))
	if _, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{span}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + 2*time.Second)
	if err := store.Prune(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTraceSummary(t.Context(), memoryTraceID); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired summary error = %v", err)
	}
	if _, err := store.GetTraceSpans(t.Context(), memoryTraceID); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired spans error = %v", err)
	}
}

func TestTraceStoreListsFilteredStableCursorPages(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := New(Options{Now: func() time.Time { return now }})
	writeSummaryTrace(t, store, "11111111111111111111111111111111", "1111111111111111", "agent-a", "session-a", "task-a", now.Add(-3*time.Minute), false, false)
	writeSummaryTrace(t, store, "22222222222222222222222222222222", "2222222222222222", "agent-b", "session-b", "task-b", now.Add(-2*time.Minute), true, false)
	writeSummaryTrace(t, store, "33333333333333333333333333333333", "3333333333333333", "agent-c", "session-c", "task-c", now.Add(-time.Minute), false, true)

	first, err := store.ListTraceSummaries(t.Context(), storage.TraceFilter{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.Items[0].TraceID != "33333333333333333333333333333333" ||
		first.Items[1].TraceID != "22222222222222222222222222222222" || first.Total != 3 || first.NextCursor == nil {
		t.Fatalf("first Trace page = %#v, %v", first, err)
	}
	writeSummaryTrace(t, store, "44444444444444444444444444444444", "4444444444444444", "agent-d", "session-d", "task-d", now, false, false)
	next, err := store.ListTraceSummaries(t.Context(), storage.TraceFilter{Cursor: *first.NextCursor, Limit: 2})
	if err != nil || len(next.Items) != 1 || next.Items[0].TraceID != "11111111111111111111111111111111" || next.Total != 3 {
		t.Fatalf("stable next Trace page = %#v, %v", next, err)
	}
	if _, err := store.ListTraceSummaries(t.Context(), storage.TraceFilter{Cursor: *first.NextCursor, Limit: 2, Status: "succeeded"}); !errors.Is(err, storage.ErrInvalidTraceCursor) {
		t.Fatalf("cursor reused with changed filter: %v", err)
	}

	hasA2A := true
	filtered, err := store.ListTraceSummaries(t.Context(), storage.TraceFilter{
		Limit: 10, AgentID: "agent-b", HasA2A: &hasA2A, Query: "SESSION-B",
		StartedAfter: timePointer(now.Add(-3 * time.Minute)), StartedBefore: timePointer(now),
	})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].TaskID != "task-b" || filtered.Total != 1 {
		t.Fatalf("filtered Trace page = %#v, %v", filtered, err)
	}
	hasError := true
	failed, err := store.ListTraceSummaries(t.Context(), storage.TraceFilter{Limit: 10, Status: "failed", HasError: &hasError})
	if err != nil || len(failed.Items) != 1 || failed.Items[0].TaskID != "task-c" {
		t.Fatalf("error Trace filter = %#v, %v", failed, err)
	}
}

func TestTraceStoreReturnsBoundedGraphAndAllUnexpiredSpanPayloads(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := New(Options{Now: func() time.Time { return now }})
	root := memoryTraceSpan("1111111111111111", "AGENT", false, now, now.Add(3*time.Second))
	root.AgentID, root.SessionID, root.TaskID = "agent", "session", "task"
	root.Attributes[telemetry.AttributeTaskRoot] = true
	root.Attributes["private.attribute"] = "attribute-secret"
	first := memoryTraceSpan("2222222222222222", "LLM", true, now.Add(time.Second), now.Add(2*time.Second))
	second := memoryTraceSpan("3333333333333333", "TOOL", true, now.Add(2*time.Second), now.Add(3*time.Second))
	expiresAt := now.Add(time.Hour)
	payloadA := json.RawMessage(`{"prompt":"one"}`)
	payloadZ := json.RawMessage(`{"completion":"two"}`)
	links := []telemetry.Link{}
	for index, span := range []telemetry.Span{root, first, second} {
		links = append(links, telemetry.Link{
			TraceID: memoryTraceID, SpanID: span.SpanID,
			LinkedTraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LinkedSpanID: []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"}[index],
			Attributes: map[string]any{"index": index},
		})
	}
	batch := telemetry.TraceBatch{
		Spans: []telemetry.Span{second, root, first}, Links: links,
		Payloads: []telemetry.Payload{
			{TraceID: memoryTraceID, SpanID: root.SpanID, Kind: "completion", PayloadJSON: payloadZ, RedactionState: telemetry.ContentStateCaptured, SizeBytes: int64(len(payloadZ)), ExpiresAt: &expiresAt},
			{TraceID: memoryTraceID, SpanID: root.SpanID, Kind: "prompt", PayloadJSON: payloadA, RedactionState: telemetry.ContentStateCaptured, SizeBytes: int64(len(payloadA)), ExpiresAt: &expiresAt},
		},
	}
	_, err := store.WriteBatch(t.Context(), batch)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.ReplayAfter(t.Context(), 0, 10)
	if err != nil || replay.Latest != 1 || len(replay.Messages) != 1 || replay.Messages[0].Topic != "trace" ||
		replay.Messages[0].EventKind != "trace" || replay.Messages[0].Trace == nil || replay.Messages[0].Trace.TraceID != memoryTraceID {
		t.Fatalf("Trace outbox replay = %#v, %v", replay, err)
	}
	document, err := json.Marshal(replay.Messages[0])
	if err != nil || bytes.Contains(document, []byte("attribute-secret")) || bytes.Contains(document, []byte(`"prompt"`)) {
		t.Fatalf("Trace outbox leaked detail: %s, %v", document, err)
	}
	if _, err := store.WriteBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	duplicateReplay, err := store.ReplayAfter(t.Context(), 1, 10)
	if err != nil || duplicateReplay.Latest != 1 || len(duplicateReplay.Messages) != 0 {
		t.Fatalf("duplicate Trace emitted outbox = %#v, %v", duplicateReplay, err)
	}
	graph, err := store.GetTraceGraph(t.Context(), memoryTraceID, storage.TraceGraphLimits{SpanLimit: 2, LinkLimit: 1})
	if err != nil || graph.TotalSpans != 3 || len(graph.Spans) != 2 || !graph.SpansTruncated ||
		graph.TotalLinks != 3 || len(graph.Links) != 1 || !graph.LinksTruncated {
		t.Fatalf("bounded Trace graph = %#v, %v", graph, err)
	}
	detail, err := store.GetTraceDetail(t.Context(), memoryTraceID, storage.TraceGraphLimits{SpanLimit: 2, LinkLimit: 1})
	if err != nil || detail.Summary.SpanCount != detail.Graph.TotalSpans || detail.RootSpan == nil ||
		detail.RootSpan.SpanID != root.SpanID || len(detail.Graph.Spans) != 2 {
		t.Fatalf("atomic Trace detail = %#v, %v", detail, err)
	}
	span, err := store.GetTraceSpan(t.Context(), memoryTraceID, root.SpanID)
	if err != nil || span.TaskID != "task" {
		t.Fatalf("single Trace Span = %#v, %v", span, err)
	}
	payloads, err := store.GetTracePayloads(t.Context(), memoryTraceID, root.SpanID)
	if err != nil || len(payloads) != 2 || payloads[0].Kind != "completion" || payloads[1].Kind != "prompt" {
		t.Fatalf("Trace Span payloads = %#v, %v", payloads, err)
	}
	spanDetail, err := store.GetTraceSpanDetail(t.Context(), memoryTraceID, root.SpanID)
	if err != nil || spanDetail.Span.SpanID != root.SpanID || len(spanDetail.Payloads) != 2 ||
		spanDetail.Payloads[0].Kind != "completion" || spanDetail.Payloads[1].Kind != "prompt" {
		t.Fatalf("atomic Trace Span detail = %#v, %v", spanDetail, err)
	}
	empty, err := store.GetTracePayloads(t.Context(), memoryTraceID, first.SpanID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty Trace Span payloads = %#v, %v", empty, err)
	}
	if _, err := store.GetTracePayloads(t.Context(), memoryTraceID, "9999999999999999"); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("missing Trace Span payload error = %v", err)
	}
}

func writeSummaryTrace(t *testing.T, store *Store, traceID, spanID, agentID, sessionID, taskID string, startedAt time.Time, hasA2A, hasError bool) {
	t.Helper()
	root := memoryTraceSpan(spanID, "AGENT", false, startedAt, startedAt.Add(time.Second))
	root.TraceID, root.AgentID, root.SessionID, root.TaskID = traceID, agentID, sessionID, taskID
	root.Attributes[telemetry.AttributeTaskRoot] = true
	spans := []telemetry.Span{root}
	if hasA2A {
		interaction := memoryTraceSpan("a"+spanID[1:], "AGENT", true, startedAt, startedAt.Add(time.Second))
		interaction.TraceID, interaction.PeerAgentID = traceID, "peer"
		interaction.Attributes["gen_ai.operation.name"] = "invoke_agent"
		spans = append(spans, interaction)
	}
	if hasError {
		root.StatusCode = telemetry.StatusError
		spans[0] = root
		interaction := memoryTraceSpan("b"+spanID[1:], "LLM", true, startedAt, startedAt.Add(time.Second))
		interaction.TraceID, interaction.StatusCode = traceID, telemetry.StatusError
		spans = append(spans, interaction)
	}
	if _, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: spans}); err != nil {
		t.Fatal(err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func memoryTraceSpan(spanID, kind string, countable bool, startedAt, endedAt time.Time) telemetry.Span {
	span := telemetry.Span{
		TraceID: memoryTraceID, SpanID: spanID, Name: kind, OpenInferenceKind: kind,
		StartedAt: startedAt, StatusCode: telemetry.StatusUnset, Countable: countable,
		ContentState: telemetry.ContentStateNotCollected, Attributes: map[string]any{},
		Resource: map[string]any{}, Events: []telemetry.Event{},
	}
	if !endedAt.IsZero() {
		span.EndedAt = &endedAt
	}
	return span
}
