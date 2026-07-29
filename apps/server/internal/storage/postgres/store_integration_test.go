package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpenDoesNotExposeDatabaseCredentials(t *testing.T) {
	const secret = "sentinel-database-password"
	store, err := Open(t.Context(), "postgresql://agentshark:"+secret+"@localhost:5432/agentshark?pool_max_conns=invalid", Options{})
	if store != nil {
		store.Close()
	}
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("safe database configuration error = %q", err)
	}
}

func TestPostgresStoreLifecycle(t *testing.T) {
	databaseURL := os.Getenv("AGENTSHARK_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENTSHARK_TEST_DATABASE_URL is not configured")
	}
	schema := fmt.Sprintf("agentshark_test_%d", time.Now().UnixNano())
	admin, err := pgx.Connect(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(t.Context(), `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		_ = admin.Close(context.Background())
	})

	now := time.Now().UTC()
	options := Options{
		MaxConnections: 4, MinConnections: 0, ConnectTimeout: 2 * time.Second,
		EventRetention: 24 * time.Hour, TraceRetention: 24 * time.Hour,
		PayloadRetention: 4 * time.Hour, OutboxRetention: 4 * time.Hour,
		Now: func() time.Time { return now },
	}
	firstPool := testPool(t, databaseURL, schema)
	store := New(firstPool, options)
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := store.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}

	first := postgresEvent("gateway:first", model.SourceAgentGateway, now.Add(-time.Minute))
	first.Raw = map[string]any{"authorization": "detail-only", "spanId": "0123456789abcdef"}
	second := postgresEvent("guard:second", model.SourceAgentGuard, now.Add(-2*time.Minute))
	attempt := now
	success := now
	checkpoint := storage.Checkpoint{
		Source: "agentgateway.logs", Cursor: json.RawMessage(`{"eventId":"first"}`),
		LastAttemptAt: &attempt, LastSuccessAt: &success,
	}
	results, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{first, second}, &checkpoint)
	if err != nil || len(results) != 2 || results[0].OutboxSequence != 1 || results[1].OutboxSequence != 2 {
		t.Fatalf("initial persist = %#v, %v", results, err)
	}
	initialPayload, err := store.GetPayload(t.Context(), results[0].EventID)
	if err != nil || initialPayload.ExpiresAt == nil {
		t.Fatalf("initial payload = %#v, %v", initialPayload, err)
	}
	duplicate, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{first}, nil)
	if err != nil || duplicate[0].Changed || duplicate[0].OutboxSequence != 0 {
		t.Fatalf("duplicate persist = %#v, %v", duplicate, err)
	}
	first.Raw["authorization"] = "updated-detail"
	rawUpdated, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{first}, nil)
	if err != nil || !rawUpdated[0].Changed || rawUpdated[0].OutboxSequence != 3 {
		t.Fatalf("raw-only updated persist = %#v, %v", rawUpdated, err)
	}
	updatedPayload, err := store.GetPayload(t.Context(), results[0].EventID)
	if err != nil || updatedPayload.ExpiresAt == nil || !updatedPayload.ExpiresAt.Equal(*initialPayload.ExpiresAt) {
		t.Fatalf("updated payload changed retention = %#v, %v", updatedPayload, err)
	}
	first.Summary = "updated summary"
	updated, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{first}, nil)
	if err != nil || !updated[0].Changed || updated[0].OutboxSequence != 4 {
		t.Fatalf("summary updated persist = %#v, %v", updated, err)
	}

	page, err := store.ListEvents(t.Context(), storage.EventFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.Total != 2 || page.Items[0].Raw != nil {
		t.Fatalf("first page = %#v, %v", page, err)
	}
	movedSecond := second
	movedSecond.Timestamp = now.Add(time.Hour)
	movedSecond.Summary = "updated second"
	moved, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{movedSecond}, nil)
	if err != nil || !moved[0].Changed || moved[0].OutboxSequence != 5 {
		t.Fatalf("stable-time update = %#v, %v", moved, err)
	}
	newer := postgresEvent("gateway:newer", model.SourceAgentGateway, now)
	if _, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{newer}, nil); err != nil {
		t.Fatal(err)
	}
	next, err := store.ListEvents(t.Context(), storage.EventFilter{Cursor: *page.NextCursor, Limit: 10})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != second.ID || next.Items[0].Summary != "updated second" ||
		!next.Items[0].Timestamp.Equal(second.Timestamp.UTC().Truncate(time.Microsecond)) || next.Total != 2 {
		t.Fatalf("stable next page = %#v, %v", next, err)
	}

	rollbackEvent := postgresEvent("gateway:rolled-back", model.SourceAgentGateway, now)
	invalidEvent := postgresEvent("", model.SourceAgentGateway, now)
	if _, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{rollbackEvent, invalidEvent}, nil); err == nil {
		t.Fatal("invalid batch unexpectedly committed")
	}
	if _, err := store.GetEvent(t.Context(), model.SourceAgentGateway, rollbackEvent.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("transaction rollback lookup = %v", err)
	}
	traceFixture := exercisePostgresTraceWrites(t, store, now)

	firstPool.Close()
	secondPool := testPool(t, databaseURL, schema)
	defer secondPool.Close()
	restarted := New(secondPool, options)
	detail, err := restarted.GetEvent(t.Context(), model.SourceAgentGateway, "first")
	if err != nil || detail.Summary != "updated summary" || detail.Raw["authorization"] != "updated-detail" {
		t.Fatalf("restart detail = %#v, %v", detail, err)
	}
	publicDetail, err := restarted.GetEvent(t.Context(), model.SourceAgentGateway, "gateway:first")
	if err != nil || publicDetail.ID != first.ID {
		t.Fatalf("restart public ID lookup = %#v, %v", publicDetail, err)
	}
	traceSummary, err := restarted.GetTraceSummary(t.Context(), traceFixture.traceID)
	if err != nil || traceSummary.Status != "succeeded" || traceSummary.Completeness != "verified" ||
		traceSummary.LLMCalls != 1 || traceSummary.MCPCalls != 1 || traceSummary.A2ACalls != 1 {
		t.Fatalf("restart trace summary = %#v, %v", traceSummary, err)
	}
	var storedPublicID, storedUpstreamID string
	if err := secondPool.QueryRow(t.Context(), `
SELECT public_id, upstream_id FROM audit_events WHERE source = $1 AND public_id = $2
`, string(model.SourceAgentGateway), first.ID).Scan(&storedPublicID, &storedUpstreamID); err != nil ||
		storedPublicID != first.ID || storedUpstreamID != first.RawRef.ID {
		t.Fatalf("stored identities = public %q upstream %q err=%v", storedPublicID, storedUpstreamID, err)
	}
	storedCheckpoint, err := restarted.GetCheckpoint(t.Context(), checkpoint.Source)
	var gotCursor, wantCursor map[string]any
	_ = json.Unmarshal(storedCheckpoint.Cursor, &gotCursor)
	_ = json.Unmarshal(checkpoint.Cursor, &wantCursor)
	if err != nil || !reflect.DeepEqual(gotCursor, wantCursor) {
		t.Fatalf("restart checkpoint = %#v, %v", storedCheckpoint, err)
	}
	replay, err := restarted.ReplayAfter(t.Context(), 1, 10)
	if err != nil || replay.Oldest != 1 || replay.Latest != 6 || len(replay.Messages) != 5 {
		t.Fatalf("restart replay = %#v, %v", replay, err)
	}
	for _, message := range replay.Messages {
		if message.Event.Raw != nil {
			t.Fatalf("outbox leaked payload: %#v", message.Event.Raw)
		}
	}

	concurrentEvent := postgresEvent("gateway:concurrent", model.SourceAgentGateway, now.Add(-3*time.Minute))
	start := make(chan struct{})
	concurrentResults := make([][]storage.PersistResult, 2)
	concurrentErrors := make([]error, 2)
	var wait sync.WaitGroup
	for index := range concurrentResults {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			concurrentResults[index], concurrentErrors[index] = restarted.PersistEvents(
				t.Context(), []model.UnifiedEvent{concurrentEvent}, nil,
			)
		}(index)
	}
	close(start)
	wait.Wait()
	changed := 0
	for index := range concurrentResults {
		if concurrentErrors[index] != nil || len(concurrentResults[index]) != 1 {
			t.Fatalf("concurrent persist %d = %#v, %v", index, concurrentResults[index], concurrentErrors[index])
		}
		if concurrentResults[index][0].Changed {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("concurrent event changes = %d, results=%#v", changed, concurrentResults)
	}
	replay, err = restarted.ReplayAfter(t.Context(), 6, 10)
	if err != nil || replay.Latest != 8 || len(replay.Messages) != 1 || replay.Messages[0].Sequence != 8 {
		t.Fatalf("concurrent replay = %#v, %v", replay, err)
	}

	type persistOutcome struct {
		results []storage.PersistResult
		err     error
	}
	slowStore := New(secondPool, options)
	fastStore := New(secondPool, options)
	slowReachedState := make(chan int64, 1)
	releaseSlow := make(chan struct{})
	var releaseSlowOnce sync.Once
	releaseSlowWriter := func() { releaseSlowOnce.Do(func() { close(releaseSlow) }) }
	defer releaseSlowWriter()
	slowStore.beforeOutboxStateUpdate = func(sequence int64) {
		slowReachedState <- sequence
		<-releaseSlow
	}
	slowOutcome := make(chan persistOutcome, 1)
	go func() {
		results, err := slowStore.PersistEvents(t.Context(), []model.UnifiedEvent{
			postgresEvent("gateway:slow-commit", model.SourceAgentGateway, now.Add(-4*time.Minute)),
		}, nil)
		slowOutcome <- persistOutcome{results: results, err: err}
	}()
	select {
	case sequence := <-slowReachedState:
		if sequence != 9 {
			t.Fatalf("slow outbox sequence = %d", sequence)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow outbox writer did not reach the commit barrier")
	}
	fastOutcome := make(chan persistOutcome, 1)
	go func() {
		results, err := fastStore.PersistEvents(t.Context(), []model.UnifiedEvent{
			postgresEvent("gateway:fast-commit", model.SourceAgentGateway, now.Add(-5*time.Minute)),
		}, nil)
		fastOutcome <- persistOutcome{results: results, err: err}
	}()
	select {
	case outcome := <-fastOutcome:
		t.Fatalf("higher sequence committed before lower sequence: %#v, %v", outcome.results, outcome.err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseSlowWriter()
	slow := <-slowOutcome
	fast := <-fastOutcome
	if slow.err != nil || fast.err != nil || len(slow.results) != 1 || len(fast.results) != 1 ||
		slow.results[0].OutboxSequence != 9 || fast.results[0].OutboxSequence != 10 {
		t.Fatalf("serialized outbox writers = slow %#v/%v fast %#v/%v", slow.results, slow.err, fast.results, fast.err)
	}
	replay, err = restarted.ReplayAfter(t.Context(), 8, 10)
	if err != nil || replay.Latest != 10 || len(replay.Messages) != 2 ||
		replay.Messages[0].Sequence != 9 || replay.Messages[1].Sequence != 10 {
		t.Fatalf("commit-ordered replay = %#v, %v", replay, err)
	}

	tightenedOptions := options
	tightenedOptions.PayloadRetention = 0
	tightenedOptions.OutboxRetention = time.Hour
	tightened := New(secondPool, tightenedOptions)
	now = now.Add(2 * time.Hour)
	if err := tightened.Prune(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := tightened.GetPayload(t.Context(), results[0].EventID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expired payload error = %v", err)
	}
	if _, err := tightened.GetTracePayload(t.Context(), traceFixture.traceID, traceFixture.rootSpanID, "task.goal"); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired trace payload error = %v", err)
	}
	traceSpans, err := tightened.GetTraceSpans(t.Context(), traceFixture.traceID)
	if err != nil || len(traceSpans) != 5 {
		t.Fatalf("trace payload prune removed span metadata: %#v, %v", traceSpans, err)
	}
	var replayedRoot telemetry.Span
	for _, span := range traceSpans {
		if span.SpanID == traceFixture.rootSpanID {
			replayedRoot = span
			break
		}
	}
	if replayedRoot.SpanID == "" || replayedRoot.ContentState != telemetry.ContentStateExpired {
		t.Fatalf("expired root span = %#v", replayedRoot)
	}
	replayedRoot.ContentState = telemetry.ContentStateCaptured
	replayedDocument := json.RawMessage(`{"goal":"replayed-private"}`)
	replayedExpiry := now.Add(time.Hour)
	replayedResult, err := tightened.WriteBatch(t.Context(), telemetry.TraceBatch{
		Spans: []telemetry.Span{replayedRoot},
		Payloads: []telemetry.Payload{{
			TraceID: traceFixture.traceID, SpanID: traceFixture.rootSpanID, Kind: "task.goal",
			ContentType: "application/json", Encoding: "identity", PayloadJSON: replayedDocument,
			RedactionState: telemetry.ContentStateCaptured, SizeBytes: int64(len(replayedDocument)),
			ExpiresAt: &replayedExpiry, CreatedAt: now,
		}},
	})
	if err != nil || replayedResult.Duplicates != 1 || replayedResult.Inserted != 0 || replayedResult.Updated != 0 {
		t.Fatalf("expired trace replay = %#v, %v", replayedResult, err)
	}
	if _, err := tightened.GetTracePayload(t.Context(), traceFixture.traceID, traceFixture.rootSpanID, "task.goal"); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired trace payload was revived: %v", err)
	}
	traceSpans, err = tightened.GetTraceSpans(t.Context(), traceFixture.traceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, span := range traceSpans {
		if span.SpanID == traceFixture.rootSpanID && span.ContentState != telemetry.ContentStateExpired {
			t.Fatalf("expired trace content state regressed: %#v", span)
		}
	}
	var tombstoneState string
	var payloadBytesNull, payloadJSONNull bool
	var tombstoneSize int64
	if err := secondPool.QueryRow(t.Context(), `
SELECT redaction_state, payload_bytes IS NULL, payload_json IS NULL, size_bytes
FROM trace_payloads
WHERE trace_id = $1 AND span_id = $2 AND payload_kind = 'task.goal'
`, traceFixture.traceID, traceFixture.rootSpanID).Scan(
		&tombstoneState, &payloadBytesNull, &payloadJSONNull, &tombstoneSize,
	); err != nil || tombstoneState != telemetry.ContentStateExpired || !payloadBytesNull || !payloadJSONNull || tombstoneSize != 0 {
		t.Fatalf("trace payload tombstone = %q/%v/%v/%d, %v", tombstoneState, payloadBytesNull, payloadJSONNull, tombstoneSize, err)
	}
	replay, err = tightened.ReplayAfter(t.Context(), 0, 10)
	if err != nil || replay.Oldest != 0 || replay.Latest != 10 || len(replay.Messages) != 0 {
		t.Fatalf("pruned replay = %#v, %v", replay, err)
	}

	now = now.Add(23 * time.Hour)
	if err := restarted.Prune(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetEvent(t.Context(), model.SourceAgentGateway, first.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expired event lookup = %v", err)
	}
	if _, err := restarted.GetTraceSummary(t.Context(), traceFixture.traceID); !errors.Is(err, storage.ErrTraceNotFound) {
		t.Fatalf("expired trace summary lookup = %v", err)
	}
	expiredCheckpoint := checkpoint
	expiredCheckpoint.Cursor = json.RawMessage(`{"eventId":"expired-never-seen"}`)
	expiredAttempt := now
	expiredCheckpoint.LastAttemptAt = &expiredAttempt
	expiredNeverSeen := postgresEvent("gateway:expired-never-seen", model.SourceAgentGateway, now.Add(-25*time.Hour))
	expiredResults, err := restarted.PersistEvents(t.Context(), []model.UnifiedEvent{first, expiredNeverSeen}, &expiredCheckpoint)
	if err != nil || len(expiredResults) != 2 || expiredResults[0].Changed || expiredResults[1].Changed ||
		expiredResults[0].EventID != "" || expiredResults[1].EventID != "" {
		t.Fatalf("expired event replay = %#v, %v", expiredResults, err)
	}
	if _, err := restarted.GetEvent(t.Context(), model.SourceAgentGateway, first.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expired event was resurrected: %v", err)
	}
	replay, err = restarted.ReplayAfter(t.Context(), 0, 10)
	if err != nil || replay.Oldest != 0 || replay.Latest != 10 || len(replay.Messages) != 0 {
		t.Fatalf("expired replay advanced outbox = %#v, %v", replay, err)
	}
	storedCheckpoint, err = restarted.GetCheckpoint(t.Context(), checkpoint.Source)
	gotCursor, wantCursor = nil, nil
	_ = json.Unmarshal(storedCheckpoint.Cursor, &gotCursor)
	_ = json.Unmarshal(expiredCheckpoint.Cursor, &wantCursor)
	if err != nil || !reflect.DeepEqual(gotCursor, wantCursor) {
		t.Fatalf("expired replay checkpoint = %#v, %v", storedCheckpoint, err)
	}
	if _, err := secondPool.Exec(t.Context(), `UPDATE agentshark_schema_migrations SET checksum = 'changed'`); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Ready(t.Context()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("readiness accepted migration checksum drift: %v", err)
	}
}

type postgresTraceFixture struct {
	traceID, rootSpanID string
}

func exercisePostgresTraceWrites(t *testing.T, store *Store, now time.Time) postgresTraceFixture {
	t.Helper()
	const traceID = "11111111111111111111111111111111"
	child := postgresTraceSpan(traceID, "2222222222222222", "LLM", true, now.Add(time.Second), now.Add(2*time.Second))
	input, output := int64(2), int64(3)
	child.InputTokens, child.OutputTokens = &input, &output
	result, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{child}})
	if err != nil || result.Inserted != 1 {
		t.Fatalf("child-first trace write = %#v, %v", result, err)
	}
	summary, err := store.GetTraceSummary(t.Context(), traceID)
	if err != nil || summary.Status != "unknown" || summary.DurationMS != nil || summary.LLMCalls != 1 {
		t.Fatalf("child-first trace summary = %#v, %v", summary, err)
	}

	root := postgresTraceSpan(traceID, "1111111111111111", "AGENT", false, now, time.Time{})
	root.AgentID, root.SessionID, root.TaskID = "agent", "session", "task"
	root.Attributes[telemetry.AttributeTaskRoot] = true
	result, err = store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{root}})
	if err != nil || result.Inserted != 1 {
		t.Fatalf("running root trace write = %#v, %v", result, err)
	}
	summary, _ = store.GetTraceSummary(t.Context(), traceID)
	if summary.Status != "running" || summary.Completeness != "partial" {
		t.Fatalf("running root summary = %#v", summary)
	}

	endedAt := now.Add(5 * time.Second)
	root.EndedAt = &endedAt
	root.StatusCode = telemetry.StatusOK
	mcp := postgresTraceSpan(traceID, "3333333333333333", "TOOL", true, now.Add(2*time.Second), now.Add(3*time.Second))
	mcp.ToolKind = "mcp"
	mcp.Attributes[telemetry.AttributeMCPMethod] = "tools/call"
	protocol := postgresTraceSpan(traceID, "4444444444444444", "TOOL", true, now.Add(2*time.Second), now.Add(3*time.Second))
	protocol.ToolKind = "mcp"
	protocol.Attributes[telemetry.AttributeMCPMethod] = "tools/list"
	a2a := postgresTraceSpan(traceID, "5555555555555555", "AGENT", true, now.Add(3*time.Second), now.Add(4*time.Second))
	a2a.PeerAgentID = "planner"
	a2a.Attributes["gen_ai.operation.name"] = "invoke_agent"
	payloadDocument := json.RawMessage(`{"goal":"private"}`)
	expiresAt := now.Add(time.Hour)
	result, err = store.WriteBatch(t.Context(), telemetry.TraceBatch{
		Spans: []telemetry.Span{root, mcp, protocol, a2a},
		Links: []telemetry.Link{{
			TraceID: traceID, SpanID: a2a.SpanID,
			LinkedTraceID: "66666666666666666666666666666666", LinkedSpanID: "7777777777777777",
			Attributes: map[string]any{"messaging.operation": "publish"},
		}},
		Payloads: []telemetry.Payload{{
			TraceID: traceID, SpanID: root.SpanID, Kind: "task.goal", ContentType: "application/json",
			Encoding: "identity", PayloadJSON: payloadDocument, RedactionState: telemetry.ContentStateCaptured,
			SizeBytes: int64(len(payloadDocument)), ExpiresAt: &expiresAt, CreatedAt: now,
		}},
	})
	if err != nil || result.Inserted != 3 || result.Updated != 1 {
		t.Fatalf("completed trace write = %#v, %v", result, err)
	}
	summary, err = store.GetTraceSummary(t.Context(), traceID)
	if err != nil || summary.Status != "succeeded" || summary.Completeness != "verified" ||
		summary.LLMCalls != 1 || summary.MCPCalls != 1 || summary.ToolCalls != 1 || summary.A2ACalls != 1 ||
		summary.TotalTokens != 5 || summary.DurationMS == nil || *summary.DurationMS != 5000 {
		t.Fatalf("completed trace summary = %#v, %v", summary, err)
	}
	links, err := store.GetTraceLinks(t.Context(), traceID)
	if err != nil || len(links) != 1 || links[0].LinkedSpanID != "7777777777777777" {
		t.Fatalf("stored trace links = %#v, %v", links, err)
	}
	payload, err := store.GetTracePayload(t.Context(), traceID, root.SpanID, "task.goal")
	if err != nil || !sameJSON(payload.PayloadJSON, payloadDocument) {
		t.Fatalf("stored trace payload = %#v, %v", payload, err)
	}
	duplicate, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{Spans: []telemetry.Span{root}})
	if err != nil || duplicate.Duplicates != 1 {
		t.Fatalf("duplicate trace write = %#v, %v", duplicate, err)
	}
	root.EndedAt = nil
	stalePayload := payload
	stalePayload.PayloadJSON = json.RawMessage(`{"goal":"stale"}`)
	stalePayload.SizeBytes = int64(len(stalePayload.PayloadJSON))
	stale, err := store.WriteBatch(t.Context(), telemetry.TraceBatch{
		Spans: []telemetry.Span{root}, Payloads: []telemetry.Payload{stalePayload},
	})
	if err != nil || stale.Duplicates != 1 {
		t.Fatalf("stale trace write = %#v, %v", stale, err)
	}
	payload, _ = store.GetTracePayload(t.Context(), traceID, root.SpanID, "task.goal")
	if !sameJSON(payload.PayloadJSON, payloadDocument) {
		t.Fatalf("stale payload replaced terminal payload: %s", payload.PayloadJSON)
	}
	return postgresTraceFixture{traceID: traceID, rootSpanID: root.SpanID}
}

func sameJSON(left, right []byte) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}

func postgresTraceSpan(traceID, spanID, kind string, countable bool, startedAt, endedAt time.Time) telemetry.Span {
	span := telemetry.Span{
		TraceID: traceID, SpanID: spanID, Name: kind, OpenInferenceKind: kind,
		StartedAt: startedAt, StatusCode: telemetry.StatusUnset, Countable: countable,
		ContentState: telemetry.ContentStateNotCollected, Attributes: map[string]any{},
		Resource: map[string]any{}, Events: []telemetry.Event{},
	}
	if !endedAt.IsZero() {
		span.EndedAt = &endedAt
	}
	return span
}

func testPool(t *testing.T, databaseURL, schema string) *pgxpool.Pool {
	t.Helper()
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	configuration.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func postgresEvent(id string, source model.Source, timestamp time.Time) model.UnifiedEvent {
	rawID := id
	if separator := strings.IndexByte(id, ':'); separator >= 0 {
		rawID = id[separator+1:]
	}
	return model.UnifiedEvent{
		ID: id, Timestamp: timestamp, Source: source, Kind: "audit", Severity: "info",
		Summary: id, RawRef: model.RawRef{Source: source, ID: rawID},
	}
}
