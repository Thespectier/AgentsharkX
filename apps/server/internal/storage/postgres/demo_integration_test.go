package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/jackc/pgx/v5"
)

func TestPostgresDemoRunLifecycle(t *testing.T) {
	databaseURL := os.Getenv("AGENTSHARK_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENTSHARK_TEST_DATABASE_URL is not configured")
	}
	schema := fmt.Sprintf("agentshark_demo_test_%d", time.Now().UnixNano())
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
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	options := Options{MaxConnections: 4, ConnectTimeout: 2 * time.Second, OutboxRetention: time.Hour, Now: func() time.Time { return now }}
	pool := testPool(t, databaseURL, schema)
	store := New(pool, options)
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	run := model.DemoRun{
		RunID: "11111111-1111-4111-8111-111111111111", Scenario: model.DemoScenarioApproval,
		Status: model.DemoRunQueued, Outcome: model.DemoOutcomeNone, RequestedAt: now,
		DelayMS: 700, TaskID: "demo-task-one", SessionID: "demo-session-one",
		RootAgentID: "demo-incident-investigator", TotalSteps: 10, FixtureVersion: "v1",
		RequestID: "request-one", CreatedBy: "admin",
	}
	created, firstSequence, err := store.CreateDemoRun(t.Context(), run, model.DemoRunEvent{RunID: run.RunID, Type: "run.status", OccurredAt: now})
	if err != nil || firstSequence < 1 || created.RunVersion != 0 {
		t.Fatalf("create = %#v sequence=%d err=%v", created, firstSequence, err)
	}
	idempotent, sequence, err := store.CreateDemoRun(t.Context(), run, model.DemoRunEvent{RunID: run.RunID, Type: "run.status", OccurredAt: now})
	if err != nil || sequence != 0 || idempotent.RunID != run.RunID {
		t.Fatalf("idempotent create = %#v sequence=%d err=%v", idempotent, sequence, err)
	}
	other := run
	other.RunID = "22222222-2222-4222-8222-222222222222"
	other.TaskID, other.SessionID, other.RequestID = "demo-task-two", "demo-session-two", "request-two"
	if _, _, err := store.CreateDemoRun(t.Context(), other, model.DemoRunEvent{}); !errors.Is(err, storage.ErrDemoRunBusy) {
		t.Fatalf("single-active error = %v", err)
	}
	updated := created
	updated.Status = model.DemoRunWaitingApproval
	updated.CurrentStep = "guarded_action"
	updated.Approval = &model.DemoApproval{
		TicketID: "ticket-one", UpstreamID: "ticket-upstream-one", Source: model.SourceAgentGuard,
		FetchedAt: now, RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "/v1/backend/approvals/0"},
		SessionID: run.SessionID,
		EventType: "tool_invoke", Phase: "tool_before", Action: "human_check",
		MatchedRules: []string{"demo_tripwire"}, Status: "pending", CreatedAt: now,
		CorrelationBasis: "session_id",
	}
	updated, secondSequence, err := store.UpdateDemoRun(t.Context(), storage.DemoMutation{
		Run: updated, ExpectedVersion: created.RunVersion,
		Event: model.DemoRunEvent{RunID: run.RunID, Type: "run.approval_linked", Approval: updated.Approval, OccurredAt: now},
	})
	if err != nil || updated.RunVersion != 1 || secondSequence <= firstSequence || updated.Approval == nil || len(updated.Approval.MatchedRules) != 1 {
		t.Fatalf("approval update = %#v sequence=%d err=%v", updated, secondSequence, err)
	}
	if _, _, err := store.UpdateDemoRun(t.Context(), storage.DemoMutation{Run: updated, ExpectedVersion: 0}); !errors.Is(err, storage.ErrDemoRunConflict) {
		t.Fatalf("stale version error = %v", err)
	}
	replay, err := store.ReplayDemoAfter(t.Context(), run.RunID, firstSequence, 10)
	if err != nil || len(replay.Messages) != 1 || replay.Messages[0].Demo == nil || replay.Messages[0].Demo.Approval == nil {
		t.Fatalf("Demo replay = %#v err=%v", replay, err)
	}
	global, err := store.ReplayAfter(t.Context(), 0, 10)
	if err != nil || len(global.Messages) != 2 || global.Messages[0].Demo == nil {
		t.Fatalf("global replay with Demo topic = %#v err=%v", global, err)
	}
	snapshot, err := store.GetDemoStreamSnapshot(t.Context(), run.RunID)
	if err != nil || snapshot.LatestSequence != secondSequence || snapshot.Run.RunVersion != 1 {
		t.Fatalf("atomic snapshot = %#v err=%v", snapshot, err)
	}
	pool.Close()
	restartedPool := testPool(t, databaseURL, schema)
	defer restartedPool.Close()
	restarted := New(restartedPool, options)
	restored, err := restarted.GetDemoRun(t.Context(), run.RunID)
	if err != nil || restored.Approval == nil || restored.Approval.TicketID != "ticket-one" ||
		restored.Approval.UpstreamID != "ticket-upstream-one" || restored.Approval.RawRef.ID != "/v1/backend/approvals/0" {
		t.Fatalf("restart restoration = %#v err=%v", restored, err)
	}
	page, err := restarted.ListDemoRuns(t.Context(), storage.DemoRunFilter{Limit: 25})
	if err != nil || len(page.Items) != 1 || page.Total != 1 {
		t.Fatalf("restart list = %#v err=%v", page, err)
	}
}
