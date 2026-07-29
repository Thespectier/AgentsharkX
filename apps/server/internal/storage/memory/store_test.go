package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
)

func TestStoreContractPersistsIdempotentlyAndUsesStableCursor(t *testing.T) {
	now := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	store := New(Options{
		EventRetention: 24 * time.Hour, PayloadRetention: 4 * time.Hour,
		OutboxRetention: 4 * time.Hour, Now: func() time.Time { return now },
	})
	first := testEvent("gateway:first", model.SourceAgentGateway, now.Add(-time.Minute))
	first.Raw = map[string]any{"authorization": "detail-only"}
	second := testEvent("guard:second", model.SourceAgentGuard, now.Add(-2*time.Minute))
	attempt := now
	success := now
	checkpoint := storage.Checkpoint{
		Source: "agentgateway.logs", Cursor: json.RawMessage(`{"eventId":"first"}`),
		LastAttemptAt: &attempt, LastSuccessAt: &success,
	}

	results, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{first, second}, &checkpoint)
	if err != nil || len(results) != 2 || !results[0].Changed || results[0].OutboxSequence != 1 {
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
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.Total != 2 {
		t.Fatalf("first page = %#v, %v", page, err)
	}
	if page.Items[0].Raw != nil || page.Items[0].Summary != "updated summary" {
		t.Fatalf("list leaked payload or stale summary: %#v", page.Items[0])
	}
	movedSecond := second
	movedSecond.Timestamp = now.Add(time.Hour)
	movedSecond.Summary = "updated second"
	moved, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{movedSecond}, nil)
	if err != nil || !moved[0].Changed || moved[0].OutboxSequence != 5 {
		t.Fatalf("stable-time update = %#v, %v", moved, err)
	}
	newer := testEvent("gateway:newer", model.SourceAgentGateway, now)
	if _, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{newer}, nil); err != nil {
		t.Fatal(err)
	}
	next, err := store.ListEvents(t.Context(), storage.EventFilter{Cursor: *page.NextCursor, Limit: 10})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != second.ID || next.Items[0].Summary != "updated second" ||
		!next.Items[0].Timestamp.Equal(second.Timestamp) || next.Total != 2 {
		t.Fatalf("stable next page = %#v, %v", next, err)
	}

	detail, err := store.GetEvent(t.Context(), model.SourceAgentGateway, "first")
	if err != nil || detail.Raw["authorization"] != "updated-detail" {
		t.Fatalf("detail payload = %#v, %v", detail.Raw, err)
	}
	if publicDetail, err := store.GetEvent(t.Context(), model.SourceAgentGateway, "gateway:first"); err != nil || publicDetail.ID != first.ID {
		t.Fatalf("public event ID lookup = %#v, %v", publicDetail, err)
	}
	storedCheckpoint, err := store.GetCheckpoint(t.Context(), checkpoint.Source)
	if err != nil || string(storedCheckpoint.Cursor) != string(checkpoint.Cursor) {
		t.Fatalf("checkpoint = %#v, %v", storedCheckpoint, err)
	}
	replay, err := store.ReplayAfter(t.Context(), 1, 10)
	if err != nil || replay.Latest != 6 || replay.Oldest != 1 || len(replay.Messages) != 5 {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
	for _, message := range replay.Messages {
		if message.Event.Raw != nil {
			t.Fatalf("outbox leaked payload: %#v", message.Event.Raw)
		}
	}

	store.options.PayloadRetention = 0
	store.options.OutboxRetention = time.Hour
	now = now.Add(2 * time.Hour)
	if err := store.Prune(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPayload(t.Context(), results[0].EventID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expired payload error = %v", err)
	}
	replay, err = store.ReplayAfter(t.Context(), 0, 10)
	if err != nil || replay.Oldest != 0 || replay.Latest != 6 || len(replay.Messages) != 0 {
		t.Fatalf("pruned replay must retain high water: %#v, %v", replay, err)
	}
}

func TestStoreDoesNotResurrectExpiredEvents(t *testing.T) {
	now := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	store := New(Options{
		EventRetention: time.Hour, OutboxRetention: time.Minute, Now: func() time.Time { return now },
	})
	checkpoint := storage.Checkpoint{Source: "agentgateway.logs", Cursor: json.RawMessage(`{"eventId":"old"}`)}
	neverSeen := testEvent("gateway:never-seen-old", model.SourceAgentGateway, now.Add(-2*time.Hour))
	results, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{neverSeen}, &checkpoint)
	if err != nil || len(results) != 1 || results[0].Changed || results[0].EventID != "" {
		t.Fatalf("never-seen expired event = %#v, %v", results, err)
	}
	if stored, err := store.GetCheckpoint(t.Context(), checkpoint.Source); err != nil || string(stored.Cursor) != string(checkpoint.Cursor) {
		t.Fatalf("expired event checkpoint = %#v, %v", stored, err)
	}
	if replay, err := store.ReplayAfter(t.Context(), 0, 10); err != nil || replay.Latest != 0 || len(replay.Messages) != 0 {
		t.Fatalf("expired event outbox = %#v, %v", replay, err)
	}

	fresh := testEvent("gateway:fresh", model.SourceAgentGateway, now)
	freshResults, err := store.PersistEvents(t.Context(), []model.UnifiedEvent{fresh}, nil)
	if err != nil || len(freshResults) != 1 || !freshResults[0].Changed || freshResults[0].OutboxSequence != 1 {
		t.Fatalf("fresh event = %#v, %v", freshResults, err)
	}
	now = now.Add(2 * time.Hour)
	if err := store.Prune(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	checkpoint.Cursor = json.RawMessage(`{"eventId":"fresh"}`)
	results, err = store.PersistEvents(t.Context(), []model.UnifiedEvent{fresh}, &checkpoint)
	if err != nil || len(results) != 1 || results[0].Changed || results[0].EventID != "" {
		t.Fatalf("expired event resurrection = %#v, %v", results, err)
	}
	if _, err := store.GetEvent(t.Context(), model.SourceAgentGateway, fresh.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expired event was resurrected: %v", err)
	}
	if replay, err := store.ReplayAfter(t.Context(), 0, 10); err != nil || replay.Latest != 1 || len(replay.Messages) != 0 {
		t.Fatalf("expired replay advanced outbox = %#v, %v", replay, err)
	}
	if stored, err := store.GetCheckpoint(t.Context(), checkpoint.Source); err != nil || string(stored.Cursor) != string(checkpoint.Cursor) {
		t.Fatalf("resurrection checkpoint = %#v, %v", stored, err)
	}
}

func testEvent(id string, source model.Source, timestamp time.Time) model.UnifiedEvent {
	return model.UnifiedEvent{
		ID: id, Timestamp: timestamp, Source: source, Kind: "audit", Severity: "info",
		Summary: id, RawRef: model.RawRef{Source: source, ID: id[len(id)-5:]},
	}
}
