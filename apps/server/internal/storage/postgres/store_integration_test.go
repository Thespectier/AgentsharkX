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
		EventRetention: 24 * time.Hour, PayloadRetention: 4 * time.Hour, OutboxRetention: 4 * time.Hour,
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
