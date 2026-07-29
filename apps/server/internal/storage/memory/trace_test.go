package memory

import (
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
