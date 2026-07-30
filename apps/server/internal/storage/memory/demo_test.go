package memory

import (
	"errors"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
)

func TestDemoRunUpdateUsesOptimisticVersionAndScopedReplay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := New(Options{Now: func() time.Time { return now }, OutboxRetention: time.Hour})
	run := model.DemoRun{
		RunID: "11111111-1111-4111-8111-111111111111", Scenario: model.DemoScenarioHappy,
		Status: model.DemoRunQueued, Outcome: model.DemoOutcomeNone, RequestedAt: now,
		TaskID: "task", SessionID: "session", RootAgentID: "agent", TotalSteps: 9,
		FixtureVersion: "v1", RequestID: "request",
	}
	created, firstSequence, err := store.CreateDemoRun(t.Context(), run, model.DemoRunEvent{RunID: run.RunID, Type: "run.status", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	updated := created
	updated.Status = model.DemoRunStarting
	updated, secondSequence, err := store.UpdateDemoRun(t.Context(), storage.DemoMutation{
		Run: updated, ExpectedVersion: 0,
		Event: model.DemoRunEvent{RunID: run.RunID, Type: "run.status", OccurredAt: now},
	})
	if err != nil || updated.RunVersion != 1 || secondSequence <= firstSequence {
		t.Fatalf("update = %#v sequence=%d err=%v", updated, secondSequence, err)
	}
	if _, _, err := store.UpdateDemoRun(t.Context(), storage.DemoMutation{Run: updated, ExpectedVersion: 0}); !errors.Is(err, storage.ErrDemoRunConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	replay, err := store.ReplayDemoAfter(t.Context(), run.RunID, firstSequence, 10)
	if err != nil || len(replay.Messages) != 1 || replay.Messages[0].EntityID != run.RunID {
		t.Fatalf("scoped replay = %#v err=%v", replay, err)
	}
}

func TestDemoRunCursorKeepsAStableWatermark(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := New(Options{Now: func() time.Time { return now }, OutboxRetention: time.Hour})
	createTerminal := func(runID, requestID string) {
		t.Helper()
		run := model.DemoRun{
			RunID: runID, Scenario: model.DemoScenarioHappy, Status: model.DemoRunQueued,
			Outcome: model.DemoOutcomeNone, RequestedAt: now, TaskID: "task-" + requestID,
			SessionID: "session-" + requestID, RootAgentID: "agent", TotalSteps: 9,
			FixtureVersion: "v1", RequestID: requestID,
		}
		created, _, err := store.CreateDemoRun(t.Context(), run, model.DemoRunEvent{RunID: runID, Type: "run.status", OccurredAt: now})
		if err != nil {
			t.Fatal(err)
		}
		created.Status = model.DemoRunSucceeded
		created.Outcome = model.DemoOutcomeNormal
		if _, _, err := store.UpdateDemoRun(t.Context(), storage.DemoMutation{Run: created, ExpectedVersion: created.RunVersion}); err != nil {
			t.Fatal(err)
		}
	}

	createTerminal("11111111-1111-4111-8111-111111111111", "one")
	createTerminal("22222222-2222-4222-8222-222222222222", "two")
	first, err := store.ListDemoRuns(t.Context(), storage.DemoRunFilter{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].RequestID != "two" || first.NextCursor == nil || first.Total != 2 {
		t.Fatalf("first page = %#v err=%v", first, err)
	}
	createTerminal("33333333-3333-4333-8333-333333333333", "three")
	second, err := store.ListDemoRuns(t.Context(), storage.DemoRunFilter{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].RequestID != "one" || second.NextCursor != nil || second.Total != 2 {
		t.Fatalf("stable second page = %#v err=%v", second, err)
	}
	if _, err := store.ListDemoRuns(t.Context(), storage.DemoRunFilter{Cursor: "invalid", Limit: 1}); !errors.Is(err, storage.ErrInvalidDemoCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}
