package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const demoRunColumns = `
run_id::text, scenario, status, outcome, COALESCE(status_reason_code, ''),
requested_at, started_at, completed_at, last_heartbeat_at, run_version, delay_ms,
task_id, session_id, COALESCE(trace_id, ''), COALESCE(root_span_id, ''), root_agent_id,
COALESCE(approval_ticket_id, ''), COALESCE(approval_status, ''), COALESCE(current_step, ''),
completed_steps, total_steps, fixture_version, request_id, COALESCE(error_code, ''),
COALESCE(error_summary, ''), COALESCE(created_by, ''), observed_metrics_json, approval_json`

func (store *Store) CreateDemoRun(ctx context.Context, run model.DemoRun, event model.DemoRunEvent) (model.DemoRun, int64, error) {
	created, sequence, err := store.createDemoRun(ctx, run, event)
	if err == nil {
		return created, sequence, nil
	}
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23505" {
		return model.DemoRun{}, 0, err
	}
	if existing, lookupErr := store.GetDemoRunByRequestID(ctx, run.RequestID); lookupErr == nil {
		return existing, 0, nil
	}
	if pgError.ConstraintName == "demo_runs_single_active" {
		return model.DemoRun{}, 0, storage.ErrDemoRunBusy
	}
	return model.DemoRun{}, 0, storage.ErrDemoRunConflict
}

func (store *Store) createDemoRun(ctx context.Context, run model.DemoRun, event model.DemoRunEvent) (model.DemoRun, int64, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if err := lockOutboxState(ctx, transaction); err != nil {
		return model.DemoRun{}, 0, err
	}
	observed, err := nullableDemoMetrics(run.ObservedMetrics)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	approval, err := nullableDemoApproval(run.Approval)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	var target demoRunScanTarget
	err = transaction.QueryRow(ctx, `
INSERT INTO demo_runs (
    run_id, scenario, status, outcome, status_reason_code, requested_at, started_at,
    completed_at, last_heartbeat_at, run_version, delay_ms, task_id, session_id,
    trace_id, root_span_id, root_agent_id, approval_ticket_id, approval_status,
    current_step, completed_steps, total_steps, fixture_version, request_id,
    error_code, error_summary, created_by, observed_metrics_json, approval_json
) VALUES (
    $1::uuid, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, 0, $10, $11, $12,
    NULLIF($13, ''), NULLIF($14, ''), $15, NULLIF($16, ''), NULLIF($17, ''),
    NULLIF($18, ''), $19, $20, $21, $22, NULLIF($23, ''), NULLIF($24, ''),
    NULLIF($25, ''), $26::jsonb, $27::jsonb
)
RETURNING `+demoRunColumns, run.RunID, string(run.Scenario), string(run.Status), string(run.Outcome),
		run.StatusReasonCode, run.RequestedAt, run.StartedAt, run.CompletedAt, run.LastHeartbeatAt,
		run.DelayMS, run.TaskID, run.SessionID, run.TraceID, run.RootSpanID, run.RootAgentID,
		approvalTicketID(run.Approval), approvalStatus(run.Approval), run.CurrentStep,
		run.CompletedSteps, run.TotalSteps, run.FixtureVersion, run.RequestID, run.ErrorCode,
		run.ErrorSummary, run.CreatedBy, observed, approval,
	).Scan(target.destinations()...)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	created, err := target.value()
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	event.RunVersion = created.RunVersion
	sequence, err := insertDemoOutbox(ctx, transaction, store, created.RunID, event)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	if err := updateOutboxState(ctx, transaction, sequence); err != nil {
		return model.DemoRun{}, 0, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return model.DemoRun{}, 0, err
	}
	return created, sequence, nil
}

func (store *Store) UpdateDemoRun(ctx context.Context, mutation storage.DemoMutation) (model.DemoRun, int64, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if err := lockOutboxState(ctx, transaction); err != nil {
		return model.DemoRun{}, 0, err
	}
	observed, err := nullableDemoMetrics(mutation.Run.ObservedMetrics)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	approval, err := nullableDemoApproval(mutation.Run.Approval)
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	var target demoRunScanTarget
	err = transaction.QueryRow(ctx, `
UPDATE demo_runs SET
    status = $3, outcome = $4, status_reason_code = NULLIF($5, ''), started_at = $6,
    completed_at = $7, last_heartbeat_at = $8, run_version = run_version + 1,
    trace_id = NULLIF($9, ''), root_span_id = NULLIF($10, ''),
    approval_ticket_id = NULLIF($11, ''), approval_status = NULLIF($12, ''),
    current_step = NULLIF($13, ''), completed_steps = $14, error_code = NULLIF($15, ''),
    error_summary = NULLIF($16, ''), observed_metrics_json = $17::jsonb,
    approval_json = $18::jsonb
WHERE run_id = $1::uuid AND run_version = $2
RETURNING `+demoRunColumns, mutation.Run.RunID, mutation.ExpectedVersion,
		string(mutation.Run.Status), string(mutation.Run.Outcome), mutation.Run.StatusReasonCode,
		mutation.Run.StartedAt, mutation.Run.CompletedAt, mutation.Run.LastHeartbeatAt,
		mutation.Run.TraceID, mutation.Run.RootSpanID, approvalTicketID(mutation.Run.Approval),
		approvalStatus(mutation.Run.Approval), mutation.Run.CurrentStep, mutation.Run.CompletedSteps,
		mutation.Run.ErrorCode, mutation.Run.ErrorSummary, observed, approval,
	).Scan(target.destinations()...)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if lookupErr := transaction.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM demo_runs WHERE run_id = $1::uuid)`, mutation.Run.RunID).Scan(&exists); lookupErr != nil {
			return model.DemoRun{}, 0, lookupErr
		}
		if exists {
			return model.DemoRun{}, 0, storage.ErrDemoRunConflict
		}
		return model.DemoRun{}, 0, storage.ErrDemoRunNotFound
	}
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	updated, err := target.value()
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	mutation.Event.RunVersion = updated.RunVersion
	sequence := int64(0)
	if mutation.Event.Type != "" {
		sequence, err = insertDemoOutbox(ctx, transaction, store, updated.RunID, mutation.Event)
		if err != nil {
			return model.DemoRun{}, 0, err
		}
		if err := updateOutboxState(ctx, transaction, sequence); err != nil {
			return model.DemoRun{}, 0, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return model.DemoRun{}, 0, err
	}
	return updated, sequence, nil
}

func (store *Store) GetDemoRun(ctx context.Context, runID string) (model.DemoRun, error) {
	return queryDemoRun(ctx, store.pool.QueryRow(ctx, `SELECT `+demoRunColumns+` FROM demo_runs WHERE run_id = $1::uuid`, strings.TrimSpace(runID)))
}

func (store *Store) GetDemoRunByRequestID(ctx context.Context, requestID string) (model.DemoRun, error) {
	return queryDemoRun(ctx, store.pool.QueryRow(ctx, `SELECT `+demoRunColumns+` FROM demo_runs WHERE request_id = $1`, strings.TrimSpace(requestID)))
}

func (store *Store) GetActiveDemoRun(ctx context.Context) (model.DemoRun, error) {
	return queryDemoRun(ctx, store.pool.QueryRow(ctx, `
SELECT `+demoRunColumns+` FROM demo_runs
WHERE status IN ('queued', 'starting', 'running', 'waiting_approval')
ORDER BY list_sequence DESC LIMIT 1`))
}

func (store *Store) ListDemoRuns(ctx context.Context, filter storage.DemoRunFilter) (storage.Page[model.DemoRun], error) {
	limit := storage.NormalizeLimit(filter.Limit)
	watermark := int64(0)
	position := int64(0)
	if filter.Cursor != "" {
		var err error
		watermark, position, err = storage.DecodeDemoCursor(filter.Cursor)
		if err != nil {
			return storage.Page[model.DemoRun]{}, err
		}
	} else if err := store.pool.QueryRow(ctx, `SELECT COALESCE(max(list_sequence), 0) FROM demo_runs`).Scan(&watermark); err != nil {
		return storage.Page[model.DemoRun]{}, err
	}
	page := storage.Page[model.DemoRun]{Items: []model.DemoRun{}}
	if watermark == 0 {
		return page, nil
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM demo_runs WHERE list_sequence <= $1`, watermark).Scan(&page.Total); err != nil {
		return storage.Page[model.DemoRun]{}, err
	}
	rows, err := store.pool.Query(ctx, `
SELECT `+demoRunColumns+`, list_sequence FROM demo_runs
WHERE list_sequence <= $1 AND ($2::bigint = 0 OR list_sequence < $2)
ORDER BY list_sequence DESC LIMIT $3`, watermark, position, limit+1)
	if err != nil {
		return storage.Page[model.DemoRun]{}, err
	}
	defer rows.Close()
	sequences := make([]int64, 0, limit+1)
	for rows.Next() {
		var target demoRunScanTarget
		var sequence int64
		destinations := append(target.destinations(), &sequence)
		if err := rows.Scan(destinations...); err != nil {
			return storage.Page[model.DemoRun]{}, err
		}
		run, err := target.value()
		if err != nil {
			return storage.Page[model.DemoRun]{}, err
		}
		page.Items = append(page.Items, run)
		sequences = append(sequences, sequence)
	}
	if err := rows.Err(); err != nil {
		return storage.Page[model.DemoRun]{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		sequences = sequences[:limit]
		next, err := storage.EncodeDemoCursor(watermark, sequences[len(sequences)-1])
		if err != nil {
			return storage.Page[model.DemoRun]{}, err
		}
		page.NextCursor = &next
	}
	return page, nil
}

func (store *Store) ReplayDemoAfter(ctx context.Context, runID string, after int64, limit int) (storage.ReplayBatch, error) {
	if limit < 1 {
		limit = 1000
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.ReplayBatch{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var exists bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM demo_runs WHERE run_id = $1::uuid)`, runID).Scan(&exists); err != nil {
		return storage.ReplayBatch{}, err
	}
	if !exists {
		return storage.ReplayBatch{}, storage.ErrDemoRunNotFound
	}
	batch := storage.ReplayBatch{Messages: []storage.OutboxMessage{}}
	if err := transaction.QueryRow(ctx, `
SELECT COALESCE(min(sequence), 0), COALESCE(max(sequence), 0)
FROM stream_outbox WHERE topic = 'demo.run' AND entity_id = $1`, runID).Scan(&batch.Oldest, &batch.Latest); err != nil {
		return storage.ReplayBatch{}, err
	}
	rows, err := transaction.Query(ctx, `
SELECT sequence, entity_id, event_kind, event_json, created_at, expires_at
FROM stream_outbox
WHERE topic = 'demo.run' AND entity_id = $1 AND sequence > $2
ORDER BY sequence ASC LIMIT $3`, runID, after, limit)
	if err != nil {
		return storage.ReplayBatch{}, err
	}
	defer rows.Close()
	for rows.Next() {
		message := storage.OutboxMessage{Topic: "demo.run"}
		var document []byte
		if err := rows.Scan(&message.Sequence, &message.EntityID, &message.EventKind, &document, &message.CreatedAt, &message.ExpiresAt); err != nil {
			return storage.ReplayBatch{}, err
		}
		var event model.DemoRunEvent
		if err := decodeJSON(document, &event); err != nil {
			return storage.ReplayBatch{}, err
		}
		message.Demo = &event
		batch.Messages = append(batch.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return storage.ReplayBatch{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return storage.ReplayBatch{}, err
	}
	return batch, nil
}

func (store *Store) GetDemoStreamSnapshot(ctx context.Context, runID string) (storage.DemoStreamSnapshot, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.DemoStreamSnapshot{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	run, err := queryDemoRun(ctx, transaction.QueryRow(ctx, `SELECT `+demoRunColumns+` FROM demo_runs WHERE run_id = $1::uuid`, runID))
	if err != nil {
		return storage.DemoStreamSnapshot{}, err
	}
	var latest int64
	if err := transaction.QueryRow(ctx, `SELECT COALESCE(max(sequence), 0) FROM stream_outbox WHERE topic = 'demo.run' AND entity_id = $1`, runID).Scan(&latest); err != nil {
		return storage.DemoStreamSnapshot{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return storage.DemoStreamSnapshot{}, err
	}
	return storage.DemoStreamSnapshot{Run: run, LatestSequence: latest}, nil
}

type demoRow interface {
	Scan(...any) error
}

func queryDemoRun(_ context.Context, row demoRow) (model.DemoRun, error) {
	var target demoRunScanTarget
	err := row.Scan(target.destinations()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DemoRun{}, storage.ErrDemoRunNotFound
	}
	if err != nil {
		return model.DemoRun{}, err
	}
	return target.value()
}

type demoRunScanTarget struct {
	run            model.DemoRun
	approvalTicket string
	approvalStatus string
	observed       []byte
	approval       []byte
}

func (target *demoRunScanTarget) destinations() []any {
	return []any{
		&target.run.RunID, &target.run.Scenario, &target.run.Status, &target.run.Outcome,
		&target.run.StatusReasonCode, &target.run.RequestedAt, &target.run.StartedAt,
		&target.run.CompletedAt, &target.run.LastHeartbeatAt, &target.run.RunVersion,
		&target.run.DelayMS, &target.run.TaskID, &target.run.SessionID, &target.run.TraceID,
		&target.run.RootSpanID, &target.run.RootAgentID, &target.approvalTicket,
		&target.approvalStatus, &target.run.CurrentStep, &target.run.CompletedSteps,
		&target.run.TotalSteps, &target.run.FixtureVersion, &target.run.RequestID,
		&target.run.ErrorCode, &target.run.ErrorSummary, &target.run.CreatedBy, &target.observed,
		&target.approval,
	}
}

func (target *demoRunScanTarget) value() (model.DemoRun, error) {
	if len(target.approval) > 0 && string(target.approval) != "null" {
		var approval model.DemoApproval
		if err := json.Unmarshal(target.approval, &approval); err != nil {
			return model.DemoRun{}, err
		}
		if approval.UpstreamID != "" && approval.Source == model.SourceAgentGuard &&
			!approval.FetchedAt.IsZero() && approval.RawRef.Source == model.SourceAgentGuard && approval.RawRef.ID != "" {
			target.run.Approval = &approval
		}
	}
	if err := decodeDemoMetrics(target.observed, &target.run); err != nil {
		return model.DemoRun{}, err
	}
	return target.run, nil
}

func decodeDemoMetrics(document []byte, run *model.DemoRun) error {
	if len(document) > 0 && string(document) != "null" {
		var metrics model.DemoMetrics
		if err := json.Unmarshal(document, &metrics); err != nil {
			return err
		}
		run.ObservedMetrics = &metrics
	}
	return nil
}

func nullableDemoMetrics(metrics *model.DemoMetrics) (any, error) {
	if metrics == nil {
		return nil, nil
	}
	document, err := json.Marshal(metrics)
	return document, err
}

func nullableDemoApproval(approval *model.DemoApproval) (any, error) {
	if approval == nil {
		return nil, nil
	}
	document, err := json.Marshal(approval)
	return document, err
}

func approvalTicketID(approval *model.DemoApproval) string {
	if approval == nil {
		return ""
	}
	return approval.TicketID
}

func approvalStatus(approval *model.DemoApproval) string {
	if approval == nil {
		return ""
	}
	return approval.Status
}

func lockOutboxState(ctx context.Context, transaction pgx.Tx) error {
	var latest int64
	return transaction.QueryRow(ctx, `SELECT latest_sequence FROM stream_outbox_state WHERE singleton = true FOR UPDATE`).Scan(&latest)
}

func insertDemoOutbox(ctx context.Context, transaction pgx.Tx, store *Store, runID string, event model.DemoRunEvent) (int64, error) {
	now := store.options.Now().UTC()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	document, err := json.Marshal(event)
	if err != nil {
		return 0, err
	}
	var sequence int64
	err = transaction.QueryRow(ctx, `
INSERT INTO stream_outbox (topic, entity_id, event_kind, event_json, created_at, expires_at)
VALUES ('demo.run', $1, $2, $3::jsonb, $4, $5)
RETURNING sequence`, runID, event.Type, document, now, now.Add(store.options.OutboxRetention)).Scan(&sequence)
	return sequence, err
}

func updateOutboxState(ctx context.Context, transaction pgx.Tx, sequence int64) error {
	command, err := transaction.Exec(ctx, `UPDATE stream_outbox_state SET latest_sequence = GREATEST(latest_sequence, $1) WHERE singleton = true`, sequence)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("committed outbox state is unavailable")
	}
	return nil
}

var _ storage.DemoStore = (*Store)(nil)
