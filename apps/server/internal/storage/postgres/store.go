// Package postgres implements AgentsharkX persistence with PostgreSQL and pgx.
package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	MaxConnections   int32
	MinConnections   int32
	ConnectTimeout   time.Duration
	EventRetention   time.Duration
	TraceRetention   time.Duration
	PayloadRetention time.Duration
	OutboxRetention  time.Duration
	Now              func() time.Time
}

type Store struct {
	pool                         *pgxpool.Pool
	options                      Options
	beforeOutboxStateUpdate      func(int64)
	afterTraceListCount          func()
	afterTraceDetailSummary      func()
	afterTraceSpanDetailSpanRead func()
}

func Open(ctx context.Context, databaseURL string, options Options) (*Store, error) {
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("parse PostgreSQL configuration")
	}
	configuration.MaxConns = options.MaxConnections
	configuration.MinConns = options.MinConnections
	configuration.ConnConfig.ConnectTimeout = options.ConnectTimeout
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, errors.New("create PostgreSQL pool")
	}
	return New(pool, options), nil
}

func New(pool *pgxpool.Pool, options Options) *Store {
	if options.EventRetention <= 0 {
		options.EventRetention = 30 * 24 * time.Hour
	}
	if options.TraceRetention <= 0 {
		options.TraceRetention = 30 * 24 * time.Hour
	}
	if options.OutboxRetention <= 0 {
		options.OutboxRetention = 24 * time.Hour
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{pool: pool, options: options}
}

func (store *Store) Close() { store.pool.Close() }

func (store *Store) PersistEvents(ctx context.Context, events []model.UnifiedEvent, checkpoint *storage.Checkpoint) ([]storage.PersistResult, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if len(events) > 0 {
		var committedLatest int64
		if err := transaction.QueryRow(ctx, `
SELECT latest_sequence FROM stream_outbox_state WHERE singleton = true FOR UPDATE
`).Scan(&committedLatest); err != nil {
			return nil, err
		}
	}
	now := store.options.Now().UTC()
	retentionCutoff := now.Add(-store.options.EventRetention).Truncate(time.Microsecond)
	results := make([]storage.PersistResult, 0, len(events))
	latestOutboxSequence := int64(0)
	for _, event := range events {
		if _, err := storage.EventIdentity(event); err != nil {
			return nil, err
		}
		upstreamID := strings.TrimSpace(event.RawRef.ID)
		columns := eventColumns(event)
		capturePayload := event.Raw != nil && store.options.PayloadRetention > 0
		eventID, existed, summaryChanged, skipped, summaryJSON, err := upsertEvent(
			ctx, transaction, event, upstreamID, columns, capturePayload, now, retentionCutoff,
		)
		if err != nil {
			return nil, err
		}
		if skipped {
			results = append(results, storage.PersistResult{})
			continue
		}
		payloadChanged := false
		if capturePayload {
			payloadJSON, err := json.Marshal(event.Raw)
			if err != nil {
				return nil, err
			}
			if !existed {
				expiresAt := now.Add(store.options.PayloadRetention)
				command, err := transaction.Exec(ctx, `
INSERT INTO audit_payloads (
    event_id, content_type, encoding, payload_json, redaction_state, size_bytes, expires_at, created_at
) VALUES ($1::uuid, 'application/json', 'identity', $2::jsonb, 'captured', $3, $4, $5)
ON CONFLICT (event_id) DO NOTHING
				`, eventID, payloadJSON, len(payloadJSON), expiresAt, now)
				if err != nil {
					return nil, err
				}
				payloadChanged = command.RowsAffected() > 0
				if payloadChanged {
					if _, err := transaction.Exec(ctx, `UPDATE audit_events SET has_payload = true WHERE id = $1::uuid`, eventID); err != nil {
						return nil, err
					}
				}
			} else {
				command, err := transaction.Exec(ctx, `
UPDATE audit_payloads SET
    content_type = 'application/json',
    encoding = 'identity',
    payload_bytes = NULL,
    payload_json = $2::jsonb,
    redaction_state = 'captured',
    size_bytes = $3
WHERE event_id = $1::uuid
  AND (expires_at IS NULL OR expires_at > $4)
  AND payload_json IS DISTINCT FROM $2::jsonb
				`, eventID, payloadJSON, len(payloadJSON), now)
				if err != nil {
					return nil, err
				}
				payloadChanged = command.RowsAffected() > 0
			}
		}
		changed := summaryChanged || payloadChanged
		if payloadChanged && !summaryChanged {
			if _, err := transaction.Exec(ctx, `UPDATE audit_events SET updated_at = $2 WHERE id = $1::uuid`, eventID, now); err != nil {
				return nil, err
			}
		}
		result := storage.PersistResult{EventID: eventID, Changed: changed}
		if changed {
			expiresAt := now.Add(store.options.OutboxRetention)
			if err := transaction.QueryRow(ctx, `
INSERT INTO stream_outbox (topic, entity_id, event_kind, event_json, created_at, expires_at)
VALUES ('audit', $1, $2, $3::jsonb, $4, $5)
RETURNING sequence
`, event.ID, event.Kind, summaryJSON, now, expiresAt).Scan(&result.OutboxSequence); err != nil {
				return nil, err
			}
			latestOutboxSequence = result.OutboxSequence
		}
		results = append(results, result)
	}
	if latestOutboxSequence > 0 {
		if store.beforeOutboxStateUpdate != nil {
			store.beforeOutboxStateUpdate(latestOutboxSequence)
		}
		command, err := transaction.Exec(ctx, `
UPDATE stream_outbox_state SET latest_sequence = GREATEST(latest_sequence, $1) WHERE singleton = true
`, latestOutboxSequence)
		if err != nil {
			return nil, err
		}
		if command.RowsAffected() != 1 {
			return nil, errors.New("committed outbox state is unavailable")
		}
	}
	if checkpoint != nil {
		if err := saveCheckpoint(ctx, transaction, *checkpoint, now); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return results, nil
}

func upsertEvent(
	ctx context.Context,
	transaction pgx.Tx,
	event model.UnifiedEvent,
	upstreamID string,
	columns eventMetadata,
	capturePayload bool,
	now time.Time,
	retentionCutoff time.Time,
) (string, bool, bool, bool, []byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var eventID string
		var occurredAt time.Time
		err := transaction.QueryRow(ctx, `
SELECT id::text, occurred_at
FROM audit_events
WHERE source = $1 AND upstream_id = $2
FOR UPDATE
`, string(event.Source), upstreamID).Scan(&eventID, &occurredAt)
		if err == nil {
			summaryEvent := storage.SummaryEvent(event)
			summaryEvent.Timestamp = occurredAt.UTC()
			summaryJSON, err := json.Marshal(summaryEvent)
			if err != nil {
				return "", false, false, false, nil, err
			}
			command, err := transaction.Exec(ctx, `
UPDATE audit_events SET
    public_id = $2,
    event_type = $3,
    severity = NULLIF($4, ''),
    status = NULLIF($5, ''),
    received_at = $6,
    trace_id = NULLIF($7, ''),
    span_id = NULLIF($8, ''),
    interaction_id = NULLIF($9, ''),
    agent_id = NULLIF($10, ''),
    session_id = NULLIF($11, ''),
    task_id = NULLIF($12, ''),
    summary_json = $13::jsonb,
    updated_at = $6
WHERE id = $1::uuid
  AND (
       public_id IS DISTINCT FROM $2
    OR event_type IS DISTINCT FROM $3
    OR severity IS DISTINCT FROM NULLIF($4, '')
    OR status IS DISTINCT FROM NULLIF($5, '')
    OR trace_id IS DISTINCT FROM NULLIF($7, '')
    OR span_id IS DISTINCT FROM NULLIF($8, '')
    OR interaction_id IS DISTINCT FROM NULLIF($9, '')
    OR agent_id IS DISTINCT FROM NULLIF($10, '')
    OR session_id IS DISTINCT FROM NULLIF($11, '')
    OR task_id IS DISTINCT FROM NULLIF($12, '')
    OR summary_json IS DISTINCT FROM $13::jsonb
  )
`, eventID, event.ID, event.Kind, event.Severity, columns.status, now,
				columns.traceID, columns.spanID, columns.interactionID, columns.agentID,
				columns.sessionID, columns.taskID, summaryJSON)
			if err != nil {
				return "", false, false, false, nil, err
			}
			if command.RowsAffected() > 1 {
				return "", false, false, false, nil, errors.New("audit event identity is not unique")
			}
			return eventID, true, command.RowsAffected() == 1, false, summaryJSON, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", false, false, false, nil, err
		}

		occurredAt = event.Timestamp.UTC().Truncate(time.Microsecond)
		if occurredAt.Before(retentionCutoff) {
			return "", false, false, true, nil, nil
		}
		summaryEvent := storage.SummaryEvent(event)
		summaryEvent.Timestamp = occurredAt
		summaryJSON, err := json.Marshal(summaryEvent)
		if err != nil {
			return "", false, false, false, nil, err
		}
		err = transaction.QueryRow(ctx, `
INSERT INTO audit_events (
    source, public_id, upstream_id, event_type, severity, status, occurred_at, received_at,
    trace_id, span_id, interaction_id, agent_id, session_id, task_id, summary_json, has_payload
) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8,
    NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''),
    NULLIF($13, ''), NULLIF($14, ''), $15::jsonb, $16)
ON CONFLICT (source, upstream_id) DO NOTHING
RETURNING id::text
`, string(event.Source), event.ID, upstreamID, event.Kind, event.Severity, columns.status,
			occurredAt, now, columns.traceID, columns.spanID, columns.interactionID,
			columns.agentID, columns.sessionID, columns.taskID, summaryJSON, capturePayload,
		).Scan(&eventID)
		if err == nil {
			return eventID, false, true, false, summaryJSON, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", false, false, false, nil, err
		}
		// Another transaction inserted the identity after this statement's
		// initial snapshot. A new READ COMMITTED statement can now lock it.
	}
	return "", false, false, false, nil, errors.New("audit event identity changed concurrently")
}

func (store *Store) ListEvents(ctx context.Context, filter storage.EventFilter) (storage.Page[model.UnifiedEvent], error) {
	limit := storage.NormalizeLimit(filter.Limit)
	watermark := int64(0)
	var cursor storage.Cursor
	var err error
	if filter.Cursor == "" {
		err = store.pool.QueryRow(ctx, `SELECT COALESCE(max(sequence), 0) FROM audit_events`).Scan(&watermark)
	} else {
		cursor, err = storage.DecodeCursor(filter.Cursor, filter.Source)
		watermark = cursor.Watermark
	}
	if err != nil {
		return storage.Page[model.UnifiedEvent]{}, err
	}
	if watermark == 0 {
		return storage.Page[model.UnifiedEvent]{Items: []model.UnifiedEvent{}}, nil
	}
	var total int
	if err := store.pool.QueryRow(ctx, `
SELECT count(*) FROM audit_events WHERE sequence <= $1 AND ($2 = '' OR source = $2)
`, watermark, string(filter.Source)).Scan(&total); err != nil {
		return storage.Page[model.UnifiedEvent]{}, err
	}
	rows, err := store.pool.Query(ctx, `
SELECT id::text, occurred_at, summary_json
FROM audit_events
WHERE sequence <= $1
  AND ($2 = '' OR source = $2)
	  AND ($3::timestamptz IS NULL OR (occurred_at, id::text) < ($3::timestamptz, $4))
ORDER BY occurred_at DESC, id DESC
LIMIT $5
`, watermark, string(filter.Source), nullableCursorTime(filter.Cursor, cursor), nullableCursorID(filter.Cursor, cursor), limit+1)
	if err != nil {
		return storage.Page[model.UnifiedEvent]{}, err
	}
	defer rows.Close()
	type row struct {
		id         string
		occurredAt time.Time
		event      model.UnifiedEvent
	}
	items := make([]row, 0, limit+1)
	for rows.Next() {
		var id string
		var occurredAt time.Time
		var document []byte
		if err := rows.Scan(&id, &occurredAt, &document); err != nil {
			return storage.Page[model.UnifiedEvent]{}, err
		}
		var event model.UnifiedEvent
		if err := decodeJSON(document, &event); err != nil {
			return storage.Page[model.UnifiedEvent]{}, err
		}
		items = append(items, row{id: id, occurredAt: occurredAt, event: event})
	}
	if err := rows.Err(); err != nil {
		return storage.Page[model.UnifiedEvent]{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := storage.Page[model.UnifiedEvent]{Items: make([]model.UnifiedEvent, 0, len(items)), Total: total}
	for _, item := range items {
		page.Items = append(page.Items, item.event)
	}
	if hasMore {
		last := items[len(items)-1]
		next, err := storage.EncodeCursor(storage.Cursor{
			Watermark: watermark, OccurredAt: last.occurredAt, ID: last.id, Source: filter.Source,
		})
		if err != nil {
			return storage.Page[model.UnifiedEvent]{}, err
		}
		page.NextCursor = &next
	}
	return page, nil
}

func (store *Store) GetEvent(ctx context.Context, source model.Source, eventID string) (model.UnifiedEvent, error) {
	var summary, payload []byte
	err := store.pool.QueryRow(ctx, `
SELECT event.summary_json, payload.payload_json
FROM audit_events AS event
LEFT JOIN audit_payloads AS payload
  ON payload.event_id = event.id AND (payload.expires_at IS NULL OR payload.expires_at > now())
WHERE event.source = $1
  AND (event.public_id = $2 OR event.upstream_id = $2)
ORDER BY event.sequence DESC
LIMIT 1
`, string(source), eventID).Scan(&summary, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.UnifiedEvent{}, storage.ErrNotFound
	}
	if err != nil {
		return model.UnifiedEvent{}, err
	}
	var event model.UnifiedEvent
	if err := decodeJSON(summary, &event); err != nil {
		return model.UnifiedEvent{}, err
	}
	if len(payload) > 0 {
		if err := decodeJSON(payload, &event.Raw); err != nil {
			return model.UnifiedEvent{}, err
		}
	}
	return event, nil
}

func (store *Store) PutPayload(ctx context.Context, payload storage.AuditPayload) error {
	if (len(payload.PayloadBytes) == 0) == (len(payload.PayloadJSON) == 0) {
		return errors.New("exactly one payload representation is required")
	}
	if payload.ContentType == "" || payload.Encoding == "" || payload.RedactionState == "" {
		return errors.New("payload metadata is required")
	}
	command, err := store.pool.Exec(ctx, `
INSERT INTO audit_payloads (
    event_id, content_type, encoding, payload_bytes, payload_json, redaction_state,
    size_bytes, expires_at, created_at
) VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7, $8, COALESCE($9, now()))
ON CONFLICT (event_id) DO UPDATE SET
    content_type = EXCLUDED.content_type,
    encoding = EXCLUDED.encoding,
    payload_bytes = EXCLUDED.payload_bytes,
    payload_json = EXCLUDED.payload_json,
    redaction_state = EXCLUDED.redaction_state,
    size_bytes = EXCLUDED.size_bytes,
    expires_at = EXCLUDED.expires_at
`, payload.EventID, payload.ContentType, payload.Encoding, nullableBytes(payload.PayloadBytes),
		nullableJSON(payload.PayloadJSON), payload.RedactionState, payload.SizeBytes, payload.ExpiresAt,
		nullableTime(payload.CreatedAt))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	_, err = store.pool.Exec(ctx, `UPDATE audit_events SET has_payload = true WHERE id = $1::uuid`, payload.EventID)
	return err
}

func (store *Store) GetPayload(ctx context.Context, eventID string) (storage.AuditPayload, error) {
	var payload storage.AuditPayload
	err := store.pool.QueryRow(ctx, `
SELECT event_id::text, content_type, encoding, payload_bytes, payload_json, redaction_state,
       size_bytes, expires_at, created_at
FROM audit_payloads
WHERE event_id = $1::uuid AND (expires_at IS NULL OR expires_at > now())
`, eventID).Scan(&payload.EventID, &payload.ContentType, &payload.Encoding, &payload.PayloadBytes,
		&payload.PayloadJSON, &payload.RedactionState, &payload.SizeBytes, &payload.ExpiresAt, &payload.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditPayload{}, storage.ErrNotFound
	}
	return payload, err
}

func (store *Store) GetCheckpoint(ctx context.Context, source string) (storage.Checkpoint, error) {
	var checkpoint storage.Checkpoint
	err := store.pool.QueryRow(ctx, `
SELECT source, cursor_json, last_success_at, last_attempt_at, COALESCE(last_error, ''), updated_at
FROM ingest_checkpoints WHERE source = $1
`, source).Scan(&checkpoint.Source, &checkpoint.Cursor, &checkpoint.LastSuccessAt, &checkpoint.LastAttemptAt,
		&checkpoint.LastError, &checkpoint.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Checkpoint{}, storage.ErrNotFound
	}
	return checkpoint, err
}

func (store *Store) SaveCheckpoint(ctx context.Context, checkpoint storage.Checkpoint) error {
	return saveCheckpoint(ctx, store.pool, checkpoint, store.options.Now().UTC())
}

func saveCheckpoint(ctx context.Context, executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, checkpoint storage.Checkpoint, now time.Time) error {
	if strings.TrimSpace(checkpoint.Source) == "" || len(checkpoint.Cursor) == 0 {
		return errors.New("checkpoint source and cursor are required")
	}
	_, err := executor.Exec(ctx, `
INSERT INTO ingest_checkpoints (
    source, cursor_json, last_success_at, last_attempt_at, last_error, updated_at
) VALUES ($1, $2::jsonb, $3, $4, NULLIF($5, ''), $6)
ON CONFLICT (source) DO UPDATE SET
    cursor_json = EXCLUDED.cursor_json,
    last_success_at = EXCLUDED.last_success_at,
    last_attempt_at = EXCLUDED.last_attempt_at,
    last_error = EXCLUDED.last_error,
    updated_at = EXCLUDED.updated_at
`, checkpoint.Source, checkpoint.Cursor, checkpoint.LastSuccessAt, checkpoint.LastAttemptAt,
		checkpoint.LastError, now)
	return err
}

func (store *Store) ReplayAfter(ctx context.Context, after int64, limit int) (storage.ReplayBatch, error) {
	if limit < 1 {
		limit = 1000
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.ReplayBatch{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	batch := storage.ReplayBatch{Messages: []storage.OutboxMessage{}}
	if err := transaction.QueryRow(ctx, `
SELECT latest_sequence FROM stream_outbox_state WHERE singleton = true
`).Scan(&batch.Latest); err != nil {
		return storage.ReplayBatch{}, err
	}
	if err := transaction.QueryRow(ctx, `SELECT COALESCE(min(sequence), 0) FROM stream_outbox`).Scan(&batch.Oldest); err != nil {
		return storage.ReplayBatch{}, err
	}
	rows, err := transaction.Query(ctx, `
SELECT sequence, topic, entity_id, event_kind, event_json, created_at, expires_at
FROM stream_outbox WHERE sequence > $1 ORDER BY sequence ASC LIMIT $2
`, after, limit)
	if err != nil {
		return storage.ReplayBatch{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var message storage.OutboxMessage
		var document []byte
		if err := rows.Scan(&message.Sequence, &message.Topic, &message.EntityID, &message.EventKind,
			&document, &message.CreatedAt, &message.ExpiresAt); err != nil {
			return storage.ReplayBatch{}, err
		}
		switch message.Topic {
		case "audit":
			if err := decodeJSON(document, &message.Event); err != nil {
				return storage.ReplayBatch{}, err
			}
		case "trace":
			var summary telemetry.Summary
			if err := decodeJSON(document, &summary); err != nil {
				return storage.ReplayBatch{}, err
			}
			message.Trace = &summary
		default:
			return storage.ReplayBatch{}, fmt.Errorf("unsupported outbox topic %q", message.Topic)
		}
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

// PruneAudit removes only BFF-owned Audit and SSE records. Keeping Trace
// retention out of this transaction allows production to grant the BFF
// read-only access to Trace tables.
func (store *Store) PruneAudit(ctx context.Context, now time.Time) error {
	now = now.UTC()
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `DELETE FROM audit_events WHERE occurred_at < $1`, now.Add(-store.options.EventRetention)); err != nil {
		return err
	}
	payloadCutoff := now
	deleteAllPayloads := store.options.PayloadRetention <= 0
	if !deleteAllPayloads {
		payloadCutoff = now.Add(-store.options.PayloadRetention)
	}
	if _, err := transaction.Exec(ctx, `
WITH expired AS (
    DELETE FROM audit_payloads
    WHERE $2::boolean
       OR (expires_at IS NOT NULL AND expires_at <= $1)
       OR created_at <= $3
    RETURNING event_id
)
UPDATE audit_events SET has_payload = false, updated_at = $1 WHERE id IN (SELECT event_id FROM expired)
`, now, deleteAllPayloads, payloadCutoff); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
DELETE FROM stream_outbox
WHERE (expires_at IS NOT NULL AND expires_at <= $1) OR created_at <= $2
`, now, now.Add(-store.options.OutboxRetention)); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

// PruneTraces removes Collector-owned Trace records and expires content
// payloads. Collector writes and retention use the same ordered advisory locks.
func (store *Store) PruneTraces(ctx context.Context, now time.Time) error {
	now = now.UTC()
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	traceCutoff := now.Add(-store.options.TraceRetention)
	rows, err := transaction.Query(ctx, `
SELECT trace_id
FROM (
    SELECT trace_id FROM trace_payloads WHERE expires_at IS NOT NULL AND expires_at <= $1
    UNION
    SELECT trace_id FROM trace_summaries WHERE last_span_at < $2
) AS affected_traces
ORDER BY trace_id
`, now, traceCutoff)
	if err != nil {
		return err
	}
	var affectedTraceIDs []string
	for rows.Next() {
		var traceID string
		if err := rows.Scan(&traceID); err != nil {
			rows.Close()
			return err
		}
		affectedTraceIDs = append(affectedTraceIDs, traceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	// Trace writers acquire the same sorted advisory locks before touching
	// payload rows. Keeping this order prevents prune/write lock inversion.
	for _, traceID := range affectedTraceIDs {
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, $2))`, traceID, traceAdvisoryLockSeed); err != nil {
			return err
		}
	}
	if _, err := transaction.Exec(ctx, `
UPDATE trace_payloads
SET payload_bytes = NULL,
    payload_json = NULL,
    redaction_state = 'expired',
    size_bytes = 0
WHERE expires_at IS NOT NULL
  AND expires_at <= $1
  AND redaction_state <> 'expired'
`, now); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
UPDATE trace_spans AS span
SET content_state = 'expired', updated_at = $1
WHERE EXISTS (
    SELECT 1 FROM trace_payloads AS payload
    WHERE payload.trace_id = span.trace_id
      AND payload.span_id = span.span_id
      AND payload.redaction_state = 'expired'
)
AND NOT EXISTS (
    SELECT 1 FROM trace_payloads AS payload
    WHERE payload.trace_id = span.trace_id
      AND payload.span_id = span.span_id
      AND payload.redaction_state <> 'expired'
)
AND span.content_state <> 'expired'
`, now); err != nil {
		return err
	}
	for _, traceID := range affectedTraceIDs {
		if _, err := transaction.Exec(ctx, `
DELETE FROM trace_spans
WHERE trace_id = $1
  AND EXISTS (
      SELECT 1 FROM trace_summaries WHERE trace_id = $1 AND last_span_at < $2
  )
`, traceID, traceCutoff); err != nil {
			return err
		}
		if _, err := transaction.Exec(ctx, `DELETE FROM trace_summaries WHERE trace_id = $1 AND last_span_at < $2`, traceID, traceCutoff); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

// Prune is retained for tests and maintenance tools that own both domains.
// Runtime services call the narrower ownership-specific methods above.
func (store *Store) Prune(ctx context.Context, now time.Time) error {
	if err := store.PruneAudit(ctx, now); err != nil {
		return err
	}
	return store.PruneTraces(ctx, now)
}

type eventMetadata struct {
	status, traceID, spanID, interactionID, agentID, sessionID, taskID string
}

func eventColumns(event model.UnifiedEvent) eventMetadata {
	metadata := eventMetadata{status: event.Decision}
	if metadata.status == "" {
		metadata.status = event.Action
	}
	if event.Correlation != nil {
		metadata.traceID = event.Correlation.TraceID
		metadata.sessionID = event.Correlation.SessionID
	}
	if event.Subject != nil {
		metadata.agentID = event.Subject.AgentID
		if metadata.sessionID == "" {
			metadata.sessionID = event.Subject.SessionID
		}
	}
	if event.Source == model.SourceAgentGateway {
		metadata.spanID = stringMapValue(event.Raw, "spanId")
	}
	return metadata
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func nullableCursorTime(raw string, cursor storage.Cursor) any {
	if raw == "" {
		return nil
	}
	return cursor.OccurredAt
}

func nullableCursorID(raw string, cursor storage.Cursor) any {
	if raw == "" {
		return nil
	}
	return cursor.ID
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func decodeJSON(document []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	return decoder.Decode(destination)
}

var _ storage.AuditStore = (*Store)(nil)
