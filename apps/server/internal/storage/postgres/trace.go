package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/assembler"
	"github.com/jackc/pgx/v5"
)

const traceAdvisoryLockSeed int64 = 0x4153485452414345

func (store *Store) WriteBatch(ctx context.Context, batch telemetry.TraceBatch) (telemetry.WriteResult, error) {
	now := store.options.Now().UTC()
	preparedSpans := make([]telemetry.Span, 0, len(batch.Spans))
	traceIDs := make(map[string]struct{}, len(batch.Spans))
	for _, span := range batch.Spans {
		prepared, err := telemetry.PrepareSpan(span, now)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		preparedSpans = append(preparedSpans, prepared)
		traceIDs[prepared.TraceID] = struct{}{}
	}
	preparedLinks := make([]telemetry.Link, 0, len(batch.Links))
	for _, link := range batch.Links {
		prepared, err := telemetry.PrepareLink(link)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		preparedLinks = append(preparedLinks, prepared)
		traceIDs[prepared.TraceID] = struct{}{}
	}
	preparedPayloads := make([]telemetry.Payload, 0, len(batch.Payloads))
	for _, payload := range batch.Payloads {
		prepared, err := telemetry.PreparePayload(payload, now)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		preparedPayloads = append(preparedPayloads, prepared)
		traceIDs[prepared.TraceID] = struct{}{}
	}
	result := telemetry.WriteResult{TraceIDs: telemetry.SortedTraceIDs(traceIDs)}
	if len(traceIDs) == 0 {
		return result, nil
	}

	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return telemetry.WriteResult{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	// A Trace-scoped transaction lock prevents two Collector instances from
	// concurrently deriving summaries from different partial snapshots.
	for _, traceID := range result.TraceIDs {
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, $2))`, traceID, traceAdvisoryLockSeed); err != nil {
			return telemetry.WriteResult{}, err
		}
	}

	affectedSummaries := make(map[string]struct{}, len(traceIDs))
	staleSpans := make(map[string]struct{})
	for _, span := range preparedSpans {
		state, stale, err := upsertTraceSpan(ctx, transaction, span)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		switch state {
		case traceInserted:
			result.Inserted++
			affectedSummaries[span.TraceID] = struct{}{}
		case traceUpdated:
			result.Updated++
			affectedSummaries[span.TraceID] = struct{}{}
		default:
			result.Duplicates++
		}
		if stale {
			staleSpans[traceSpanKey(span.TraceID, span.SpanID)] = struct{}{}
		}
	}
	for _, link := range preparedLinks {
		if _, blocked := staleSpans[traceSpanKey(link.TraceID, link.SpanID)]; blocked {
			continue
		}
		attributesJSON, err := json.Marshal(link.Attributes)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		if _, err := transaction.Exec(ctx, `
INSERT INTO trace_links (trace_id, span_id, linked_trace_id, linked_span_id, attributes_json)
VALUES ($1, $2, $3, $4, $5::jsonb)
ON CONFLICT (trace_id, span_id, linked_trace_id, linked_span_id) DO UPDATE SET
    attributes_json = EXCLUDED.attributes_json
WHERE trace_links.attributes_json IS DISTINCT FROM EXCLUDED.attributes_json
`, link.TraceID, link.SpanID, link.LinkedTraceID, link.LinkedSpanID, attributesJSON); err != nil {
			return telemetry.WriteResult{}, err
		}
	}
	for _, payload := range preparedPayloads {
		if _, blocked := staleSpans[traceSpanKey(payload.TraceID, payload.SpanID)]; blocked {
			continue
		}
		if _, err := transaction.Exec(ctx, `
INSERT INTO trace_payloads (
    trace_id, span_id, payload_kind, content_type, encoding, payload_bytes,
    payload_json, redaction_state, size_bytes, expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11)
ON CONFLICT (trace_id, span_id, payload_kind) DO UPDATE SET
    content_type = EXCLUDED.content_type,
    encoding = EXCLUDED.encoding,
    payload_bytes = EXCLUDED.payload_bytes,
    payload_json = EXCLUDED.payload_json,
    redaction_state = EXCLUDED.redaction_state,
    size_bytes = EXCLUDED.size_bytes
WHERE (trace_payloads.expires_at IS NULL OR trace_payloads.expires_at > $12)
  AND trace_payloads.redaction_state <> 'expired'
  AND (
       trace_payloads.content_type IS DISTINCT FROM EXCLUDED.content_type
    OR trace_payloads.encoding IS DISTINCT FROM EXCLUDED.encoding
    OR trace_payloads.payload_bytes IS DISTINCT FROM EXCLUDED.payload_bytes
    OR trace_payloads.payload_json IS DISTINCT FROM EXCLUDED.payload_json
    OR trace_payloads.redaction_state IS DISTINCT FROM EXCLUDED.redaction_state
    OR trace_payloads.size_bytes IS DISTINCT FROM EXCLUDED.size_bytes
  )
`, payload.TraceID, payload.SpanID, payload.Kind, payload.ContentType, payload.Encoding,
			nullableBytes(payload.PayloadBytes), nullableJSON(payload.PayloadJSON), payload.RedactionState,
			payload.SizeBytes, payload.ExpiresAt, payload.CreatedAt, now); err != nil {
			return telemetry.WriteResult{}, err
		}
	}
	latestOutboxSequence := int64(0)
	if len(affectedSummaries) > 0 {
		var committedLatest int64
		if err := transaction.QueryRow(ctx, `
SELECT latest_sequence FROM stream_outbox_state WHERE singleton = true FOR UPDATE
`).Scan(&committedLatest); err != nil {
			return telemetry.WriteResult{}, err
		}
	}
	for _, traceID := range telemetry.SortedTraceIDs(affectedSummaries) {
		spans, err := getTraceSpans(ctx, transaction, traceID)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		summary, err := assembler.Assemble(traceID, spans, now)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		if err := upsertTraceSummary(ctx, transaction, summary); err != nil {
			return telemetry.WriteResult{}, err
		}
		summaryJSON, err := json.Marshal(summary)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		expiresAt := now.Add(store.options.OutboxRetention)
		if err := transaction.QueryRow(ctx, `
INSERT INTO stream_outbox (topic, entity_id, event_kind, event_json, created_at, expires_at)
VALUES ('trace', $1, 'trace', $2::jsonb, $3, $4)
RETURNING sequence
`, traceID, summaryJSON, now, expiresAt).Scan(&latestOutboxSequence); err != nil {
			return telemetry.WriteResult{}, err
		}
	}
	if latestOutboxSequence > 0 {
		if store.beforeOutboxStateUpdate != nil {
			store.beforeOutboxStateUpdate(latestOutboxSequence)
		}
		command, err := transaction.Exec(ctx, `
UPDATE stream_outbox_state SET latest_sequence = GREATEST(latest_sequence, $1) WHERE singleton = true
`, latestOutboxSequence)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		if command.RowsAffected() != 1 {
			return telemetry.WriteResult{}, errors.New("committed outbox state is unavailable")
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return telemetry.WriteResult{}, err
	}
	return result, nil
}

func (store *Store) ListTraceSummaries(ctx context.Context, filter storage.TraceFilter) (storage.Page[telemetry.Summary], error) {
	filter = storage.NormalizeTraceFilter(filter)
	limit := storage.NormalizeLimit(filter.Limit)
	watermark := int64(0)
	position := int64(0)
	var err error
	if filter.Cursor != "" {
		watermark, position, err = storage.DecodeTraceCursor(filter.Cursor, filter)
		if err != nil {
			return storage.Page[telemetry.Summary]{}, err
		}
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.Page[telemetry.Summary]{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if filter.Cursor == "" {
		err = transaction.QueryRow(ctx, `SELECT COALESCE(max(list_sequence), 0) FROM trace_summaries`).Scan(&watermark)
	}
	if err != nil {
		return storage.Page[telemetry.Summary]{}, err
	}
	if watermark == 0 {
		if err := transaction.Commit(ctx); err != nil {
			return storage.Page[telemetry.Summary]{}, err
		}
		return storage.Page[telemetry.Summary]{Items: []telemetry.Summary{}}, nil
	}

	parameters := []any{
		watermark, filter.Status, filter.Completeness, filter.AgentID, filter.SessionID, filter.TaskID,
		nullableTraceBool(filter.HasError), nullableTraceBool(filter.HasA2A), filter.StartedAfter,
		filter.StartedBefore, filter.Query,
	}
	var total int
	if err := transaction.QueryRow(ctx, `
SELECT count(*)
FROM trace_summaries
WHERE list_sequence <= $1
  AND ($2 = '' OR status = $2)
  AND ($3 = '' OR completeness = $3)
  AND ($4 = '' OR root_agent_id = $4)
  AND ($5 = '' OR session_id = $5)
  AND ($6 = '' OR task_id = $6)
  AND ($7::boolean IS NULL OR (error_count > 0) = $7)
  AND ($8::boolean IS NULL OR (a2a_calls > 0) = $8)
  AND ($9::timestamptz IS NULL OR started_at >= $9)
  AND ($10::timestamptz IS NULL OR started_at < $10)
  AND ($11 = '' OR strpos(lower(trace_id), lower($11)) > 0
       OR strpos(lower(COALESCE(task_id, '')), lower($11)) > 0
       OR strpos(lower(COALESCE(session_id, '')), lower($11)) > 0)
	`, parameters...).Scan(&total); err != nil {
		return storage.Page[telemetry.Summary]{}, err
	}
	if store.afterTraceListCount != nil {
		store.afterTraceListCount()
	}
	rows, err := transaction.Query(ctx, traceSummarySelect+`
WHERE list_sequence <= $1
  AND ($2 = '' OR status = $2)
  AND ($3 = '' OR completeness = $3)
  AND ($4 = '' OR root_agent_id = $4)
  AND ($5 = '' OR session_id = $5)
  AND ($6 = '' OR task_id = $6)
  AND ($7::boolean IS NULL OR (error_count > 0) = $7)
  AND ($8::boolean IS NULL OR (a2a_calls > 0) = $8)
  AND ($9::timestamptz IS NULL OR started_at >= $9)
  AND ($10::timestamptz IS NULL OR started_at < $10)
  AND ($11 = '' OR strpos(lower(trace_id), lower($11)) > 0
       OR strpos(lower(COALESCE(task_id, '')), lower($11)) > 0
       OR strpos(lower(COALESCE(session_id, '')), lower($11)) > 0)
  AND ($12::bigint = 0 OR list_sequence < $12)
ORDER BY list_sequence DESC
LIMIT $13
`, append(parameters, position, limit+1)...)
	if err != nil {
		return storage.Page[telemetry.Summary]{}, err
	}
	defer rows.Close()
	type item struct {
		summary  telemetry.Summary
		sequence int64
	}
	items := make([]item, 0, limit+1)
	for rows.Next() {
		summary, sequence, err := scanTraceSummarySequence(rows)
		if err != nil {
			return storage.Page[telemetry.Summary]{}, err
		}
		items = append(items, item{summary: summary, sequence: sequence})
	}
	if err := rows.Err(); err != nil {
		return storage.Page[telemetry.Summary]{}, err
	}
	rows.Close()
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := storage.Page[telemetry.Summary]{Items: make([]telemetry.Summary, 0, len(items)), Total: total}
	for _, item := range items {
		page.Items = append(page.Items, item.summary)
	}
	if hasMore {
		next, err := storage.EncodeTraceCursor(watermark, items[len(items)-1].sequence, filter)
		if err != nil {
			return storage.Page[telemetry.Summary]{}, err
		}
		page.NextCursor = &next
	}
	if err := transaction.Commit(ctx); err != nil {
		return storage.Page[telemetry.Summary]{}, err
	}
	return page, nil
}

type traceWriteState int

const (
	traceDuplicate traceWriteState = iota
	traceInserted
	traceUpdated
)

func upsertTraceSpan(ctx context.Context, transaction pgx.Tx, incoming telemetry.Span) (traceWriteState, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		existing, err := getTraceSpan(ctx, transaction, incoming.TraceID, incoming.SpanID, true)
		if err == nil {
			stale := existing.EndedAt != nil && (incoming.EndedAt == nil || incoming.EndedAt.Before(*existing.EndedAt))
			merged := telemetry.MergeSpan(existing, incoming)
			if telemetry.EqualSpan(existing, merged) {
				return traceDuplicate, stale, nil
			}
			if err := updateTraceSpan(ctx, transaction, merged); err != nil {
				return traceDuplicate, false, err
			}
			return traceUpdated, false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return traceDuplicate, false, err
		}
		inserted, err := insertTraceSpan(ctx, transaction, incoming)
		if err != nil {
			return traceDuplicate, false, err
		}
		if inserted {
			return traceInserted, false, nil
		}
	}
	return traceDuplicate, false, errors.New("trace span identity changed concurrently")
}

func insertTraceSpan(ctx context.Context, transaction pgx.Tx, span telemetry.Span) (bool, error) {
	attributesJSON, resourceJSON, eventsJSON, err := traceDocuments(span)
	if err != nil {
		return false, err
	}
	command, err := transaction.Exec(ctx, `
INSERT INTO trace_spans (
    trace_id, span_id, parent_span_id, trace_state, span_name, openinference_kind,
    otel_span_kind, started_at, ended_at, duration_ms, status_code, status_message,
    agent_id, session_id, task_id, provider, model, tool_name, tool_kind, mcp_server,
    peer_agent_id, input_tokens, output_tokens, total_tokens, countable, content_state,
    attributes_json, resource_json, events_json, instrumentation_scope,
    instrumentation_version, semantic_convention_version, received_at, updated_at
) VALUES (
    $1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, NULLIF($6, ''), $7, $8, $9, $10,
    $11, NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''),
    NULLIF($16, ''), NULLIF($17, ''), NULLIF($18, ''), NULLIF($19, ''), NULLIF($20, ''),
    NULLIF($21, ''), $22, $23, $24, $25, $26, $27::jsonb, $28::jsonb, $29::jsonb,
    NULLIF($30, ''), NULLIF($31, ''), NULLIF($32, ''), $33, $34
)
ON CONFLICT (trace_id, span_id) DO NOTHING
`, span.TraceID, span.SpanID, span.ParentSpanID, span.TraceState, span.Name, span.OpenInferenceKind,
		span.OTelSpanKind, span.StartedAt, span.EndedAt, span.DurationMS, span.StatusCode, span.StatusMessage,
		span.AgentID, span.SessionID, span.TaskID, span.Provider, span.Model, span.ToolName, span.ToolKind,
		span.MCPServer, span.PeerAgentID, span.InputTokens, span.OutputTokens, span.TotalTokens,
		span.Countable, span.ContentState, attributesJSON, resourceJSON, eventsJSON,
		span.InstrumentationScope, span.InstrumentationVersion, span.SemanticConventionVersion,
		span.ReceivedAt, span.UpdatedAt)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

func updateTraceSpan(ctx context.Context, transaction pgx.Tx, span telemetry.Span) error {
	attributesJSON, resourceJSON, eventsJSON, err := traceDocuments(span)
	if err != nil {
		return err
	}
	command, err := transaction.Exec(ctx, `
UPDATE trace_spans SET
    parent_span_id = NULLIF($3, ''), trace_state = NULLIF($4, ''), span_name = $5,
    openinference_kind = NULLIF($6, ''), otel_span_kind = $7, started_at = $8,
    ended_at = $9, duration_ms = $10, status_code = $11, status_message = NULLIF($12, ''),
    agent_id = NULLIF($13, ''), session_id = NULLIF($14, ''), task_id = NULLIF($15, ''),
    provider = NULLIF($16, ''), model = NULLIF($17, ''), tool_name = NULLIF($18, ''),
    tool_kind = NULLIF($19, ''), mcp_server = NULLIF($20, ''), peer_agent_id = NULLIF($21, ''),
    input_tokens = $22, output_tokens = $23, total_tokens = $24, countable = $25,
    content_state = $26, attributes_json = $27::jsonb, resource_json = $28::jsonb,
    events_json = $29::jsonb, instrumentation_scope = NULLIF($30, ''),
    instrumentation_version = NULLIF($31, ''), semantic_convention_version = NULLIF($32, ''),
    updated_at = $33
WHERE trace_id = $1 AND span_id = $2
`, span.TraceID, span.SpanID, span.ParentSpanID, span.TraceState, span.Name, span.OpenInferenceKind,
		span.OTelSpanKind, span.StartedAt, span.EndedAt, span.DurationMS, span.StatusCode, span.StatusMessage,
		span.AgentID, span.SessionID, span.TaskID, span.Provider, span.Model, span.ToolName, span.ToolKind,
		span.MCPServer, span.PeerAgentID, span.InputTokens, span.OutputTokens, span.TotalTokens,
		span.Countable, span.ContentState, attributesJSON, resourceJSON, eventsJSON,
		span.InstrumentationScope, span.InstrumentationVersion, span.SemanticConventionVersion, span.UpdatedAt)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return storage.ErrTraceNotFound
	}
	return nil
}

func traceDocuments(span telemetry.Span) ([]byte, []byte, []byte, error) {
	attributesJSON, err := json.Marshal(span.Attributes)
	if err != nil {
		return nil, nil, nil, err
	}
	resourceJSON, err := json.Marshal(span.Resource)
	if err != nil {
		return nil, nil, nil, err
	}
	eventsJSON, err := json.Marshal(span.Events)
	if err != nil {
		return nil, nil, nil, err
	}
	return attributesJSON, resourceJSON, eventsJSON, nil
}

func (store *Store) GetTraceSpans(ctx context.Context, traceID string) ([]telemetry.Span, error) {
	if !telemetry.ValidTraceID(traceID) {
		return nil, storage.ErrTraceNotFound
	}
	return getTraceSpans(ctx, store.pool, traceID)
}

type traceQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func getTraceSpans(ctx context.Context, queryer traceQueryer, traceID string) ([]telemetry.Span, error) {
	rows, err := queryer.Query(ctx, traceSpanSelect+`
WHERE trace_id = $1
ORDER BY started_at, span_id
`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spans := []telemetry.Span{}
	for rows.Next() {
		span, err := scanTraceSpan(rows)
		if err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, storage.ErrTraceNotFound
	}
	return spans, nil
}

type traceRowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getTraceSpan(ctx context.Context, queryer traceRowQueryer, traceID, spanID string, lock bool) (telemetry.Span, error) {
	query := traceSpanSelect + ` WHERE trace_id = $1 AND span_id = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanTraceSpan(queryer.QueryRow(ctx, query, traceID, spanID))
}

const traceSpanSelect = `
SELECT trace_id, span_id, COALESCE(parent_span_id, ''), COALESCE(trace_state, ''), span_name,
       COALESCE(openinference_kind, ''), COALESCE(otel_span_kind, 0), started_at, ended_at,
       duration_ms, status_code, COALESCE(status_message, ''), COALESCE(agent_id, ''),
       COALESCE(session_id, ''), COALESCE(task_id, ''), COALESCE(provider, ''),
       COALESCE(model, ''), COALESCE(tool_name, ''), COALESCE(tool_kind, ''),
       COALESCE(mcp_server, ''), COALESCE(peer_agent_id, ''), input_tokens, output_tokens,
       total_tokens, countable, content_state, attributes_json, resource_json, events_json,
       COALESCE(instrumentation_scope, ''), COALESCE(instrumentation_version, ''),
       COALESCE(semantic_convention_version, ''), received_at, updated_at
FROM trace_spans`

type traceRow interface {
	Scan(...any) error
}

func scanTraceSpan(row traceRow) (telemetry.Span, error) {
	var span telemetry.Span
	var attributesJSON, resourceJSON, eventsJSON []byte
	if err := row.Scan(
		&span.TraceID, &span.SpanID, &span.ParentSpanID, &span.TraceState, &span.Name,
		&span.OpenInferenceKind, &span.OTelSpanKind, &span.StartedAt, &span.EndedAt,
		&span.DurationMS, &span.StatusCode, &span.StatusMessage, &span.AgentID, &span.SessionID,
		&span.TaskID, &span.Provider, &span.Model, &span.ToolName, &span.ToolKind, &span.MCPServer,
		&span.PeerAgentID, &span.InputTokens, &span.OutputTokens, &span.TotalTokens, &span.Countable,
		&span.ContentState, &attributesJSON, &resourceJSON, &eventsJSON, &span.InstrumentationScope,
		&span.InstrumentationVersion, &span.SemanticConventionVersion, &span.ReceivedAt, &span.UpdatedAt,
	); err != nil {
		return telemetry.Span{}, err
	}
	if err := decodeJSON(attributesJSON, &span.Attributes); err != nil {
		return telemetry.Span{}, err
	}
	if err := decodeJSON(resourceJSON, &span.Resource); err != nil {
		return telemetry.Span{}, err
	}
	if err := decodeJSON(eventsJSON, &span.Events); err != nil {
		return telemetry.Span{}, err
	}
	return span, nil
}

func (store *Store) GetTraceSpan(ctx context.Context, traceID, spanID string) (telemetry.Span, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return telemetry.Span{}, storage.ErrTraceNotFound
	}
	span, err := getTraceSpan(ctx, store.pool, traceID, spanID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return telemetry.Span{}, storage.ErrTraceNotFound
	}
	return span, err
}

func (store *Store) GetTraceLinks(ctx context.Context, traceID string) ([]telemetry.Link, error) {
	if !telemetry.ValidTraceID(traceID) {
		return nil, storage.ErrTraceNotFound
	}
	rows, err := store.pool.Query(ctx, `
SELECT trace_id, span_id, linked_trace_id, linked_span_id, attributes_json
FROM trace_links WHERE trace_id = $1 ORDER BY span_id, linked_trace_id, linked_span_id
`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []telemetry.Link{}
	for rows.Next() {
		var link telemetry.Link
		var attributesJSON []byte
		if err := rows.Scan(&link.TraceID, &link.SpanID, &link.LinkedTraceID, &link.LinkedSpanID, &attributesJSON); err != nil {
			return nil, err
		}
		if err := decodeJSON(attributesJSON, &link.Attributes); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

func (store *Store) GetTraceGraph(ctx context.Context, traceID string, limits storage.TraceGraphLimits) (storage.TraceGraph, error) {
	if !telemetry.ValidTraceID(traceID) {
		return storage.TraceGraph{}, storage.ErrTraceNotFound
	}
	limits = storage.NormalizeTraceGraphLimits(limits)
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.TraceGraph{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	graph, err := getTraceGraph(ctx, transaction, traceID, limits, true)
	if err != nil {
		return storage.TraceGraph{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return storage.TraceGraph{}, err
	}
	return graph, nil
}

type traceReadQueryer interface {
	traceQueryer
	traceRowQueryer
}

func getTraceGraph(
	ctx context.Context,
	queryer traceReadQueryer,
	traceID string,
	limits storage.TraceGraphLimits,
	requireSummary bool,
) (storage.TraceGraph, error) {
	var exists bool
	if requireSummary {
		if err := queryer.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM trace_summaries WHERE trace_id = $1)`, traceID).Scan(&exists); err != nil {
			return storage.TraceGraph{}, err
		}
		if !exists {
			return storage.TraceGraph{}, storage.ErrTraceNotFound
		}
	}

	graph := storage.TraceGraph{Spans: []telemetry.Span{}, Links: []telemetry.Link{}}
	if err := queryer.QueryRow(ctx, `SELECT count(*) FROM trace_spans WHERE trace_id = $1`, traceID).Scan(&graph.TotalSpans); err != nil {
		return storage.TraceGraph{}, err
	}
	if err := queryer.QueryRow(ctx, `SELECT count(*) FROM trace_links WHERE trace_id = $1`, traceID).Scan(&graph.TotalLinks); err != nil {
		return storage.TraceGraph{}, err
	}
	spanRows, err := queryer.Query(ctx, traceSpanSelect+`
WHERE trace_id = $1
ORDER BY started_at, span_id
LIMIT $2
`, traceID, limits.SpanLimit)
	if err != nil {
		return storage.TraceGraph{}, err
	}
	selectedSpanIDs := make([]string, 0, limits.SpanLimit)
	for spanRows.Next() {
		span, err := scanTraceSpan(spanRows)
		if err != nil {
			spanRows.Close()
			return storage.TraceGraph{}, err
		}
		graph.Spans = append(graph.Spans, span)
		selectedSpanIDs = append(selectedSpanIDs, span.SpanID)
	}
	if err := spanRows.Err(); err != nil {
		spanRows.Close()
		return storage.TraceGraph{}, err
	}
	spanRows.Close()
	graph.SpansTruncated = len(graph.Spans) < graph.TotalSpans

	if len(selectedSpanIDs) > 0 && graph.TotalLinks > 0 {
		linkRows, err := queryer.Query(ctx, `
SELECT trace_id, span_id, linked_trace_id, linked_span_id, attributes_json
FROM trace_links
WHERE trace_id = $1 AND span_id = ANY($2::text[])
ORDER BY span_id, linked_trace_id, linked_span_id
LIMIT $3
`, traceID, selectedSpanIDs, limits.LinkLimit)
		if err != nil {
			return storage.TraceGraph{}, err
		}
		for linkRows.Next() {
			link, err := scanTraceLink(linkRows)
			if err != nil {
				linkRows.Close()
				return storage.TraceGraph{}, err
			}
			graph.Links = append(graph.Links, link)
		}
		if err := linkRows.Err(); err != nil {
			linkRows.Close()
			return storage.TraceGraph{}, err
		}
		linkRows.Close()
	}
	graph.LinksTruncated = len(graph.Links) < graph.TotalLinks
	return graph, nil
}

func (store *Store) GetTraceDetail(ctx context.Context, traceID string, limits storage.TraceGraphLimits) (storage.TraceDetail, error) {
	if !telemetry.ValidTraceID(traceID) {
		return storage.TraceDetail{}, storage.ErrTraceNotFound
	}
	limits = storage.NormalizeTraceGraphLimits(limits)
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.TraceDetail{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	summary, _, err := scanTraceSummarySequence(transaction.QueryRow(ctx, traceSummarySelect+` WHERE trace_id = $1`, traceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.TraceDetail{}, storage.ErrTraceNotFound
	}
	if err != nil {
		return storage.TraceDetail{}, err
	}
	if store.afterTraceDetailSummary != nil {
		store.afterTraceDetailSummary()
	}
	graph, err := getTraceGraph(ctx, transaction, traceID, limits, false)
	if err != nil {
		return storage.TraceDetail{}, err
	}
	detail := storage.TraceDetail{Summary: summary, Graph: graph}
	if summary.RootSpanID != "" {
		root, err := getTraceSpan(ctx, transaction, traceID, summary.RootSpanID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.TraceDetail{}, storage.ErrTraceNotFound
		}
		if err != nil {
			return storage.TraceDetail{}, err
		}
		detail.RootSpan = &root
	}
	if err := transaction.Commit(ctx); err != nil {
		return storage.TraceDetail{}, err
	}
	return detail, nil
}

func scanTraceLink(row traceRow) (telemetry.Link, error) {
	var link telemetry.Link
	var attributesJSON []byte
	if err := row.Scan(&link.TraceID, &link.SpanID, &link.LinkedTraceID, &link.LinkedSpanID, &attributesJSON); err != nil {
		return telemetry.Link{}, err
	}
	if err := decodeJSON(attributesJSON, &link.Attributes); err != nil {
		return telemetry.Link{}, err
	}
	return link, nil
}

func upsertTraceSummary(ctx context.Context, transaction pgx.Tx, summary telemetry.Summary) error {
	_, err := transaction.Exec(ctx, `
INSERT INTO trace_summaries (
    trace_id, task_id, session_id, root_agent_id, root_span_id, status, completeness,
    started_at, ended_at, duration_ms, llm_calls, tool_calls, mcp_calls, local_tool_calls,
    a2a_calls, retriever_calls, input_tokens, output_tokens, total_tokens, error_count,
    risk_level, span_count, last_span_at, updated_at
) VALUES (
    $1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7,
    $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
    NULLIF($21, ''), $22, $23, $24
)
ON CONFLICT (trace_id) DO UPDATE SET
    task_id = EXCLUDED.task_id, session_id = EXCLUDED.session_id,
    root_agent_id = EXCLUDED.root_agent_id, root_span_id = EXCLUDED.root_span_id,
    status = EXCLUDED.status, completeness = EXCLUDED.completeness,
    started_at = EXCLUDED.started_at, ended_at = EXCLUDED.ended_at,
    duration_ms = EXCLUDED.duration_ms, llm_calls = EXCLUDED.llm_calls,
    tool_calls = EXCLUDED.tool_calls, mcp_calls = EXCLUDED.mcp_calls,
    local_tool_calls = EXCLUDED.local_tool_calls, a2a_calls = EXCLUDED.a2a_calls,
    retriever_calls = EXCLUDED.retriever_calls, input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens, total_tokens = EXCLUDED.total_tokens,
    error_count = EXCLUDED.error_count,
    risk_level = COALESCE(EXCLUDED.risk_level, trace_summaries.risk_level),
    span_count = EXCLUDED.span_count, last_span_at = EXCLUDED.last_span_at,
    updated_at = EXCLUDED.updated_at
`, summary.TraceID, summary.TaskID, summary.SessionID, summary.RootAgentID, summary.RootSpanID,
		summary.Status, summary.Completeness, summary.StartedAt, summary.EndedAt, summary.DurationMS,
		summary.LLMCalls, summary.ToolCalls, summary.MCPCalls, summary.LocalToolCalls, summary.A2ACalls,
		summary.RetrieverCalls, summary.InputTokens, summary.OutputTokens, summary.TotalTokens,
		summary.ErrorCount, summary.RiskLevel, summary.SpanCount, summary.LastSpanAt, summary.UpdatedAt)
	return err
}

const traceSummarySelect = `
SELECT trace_id, COALESCE(task_id, ''), COALESCE(session_id, ''), COALESCE(root_agent_id, ''),
       COALESCE(root_span_id, ''), status, completeness, started_at, ended_at, duration_ms,
       llm_calls, tool_calls, mcp_calls, local_tool_calls, a2a_calls, retriever_calls,
       input_tokens, output_tokens, total_tokens, error_count, COALESCE(risk_level, ''),
       span_count, last_span_at, updated_at, list_sequence
FROM trace_summaries`

func scanTraceSummarySequence(row traceRow) (telemetry.Summary, int64, error) {
	var summary telemetry.Summary
	var sequence int64
	err := row.Scan(
		&summary.TraceID, &summary.TaskID, &summary.SessionID, &summary.RootAgentID, &summary.RootSpanID,
		&summary.Status, &summary.Completeness, &summary.StartedAt, &summary.EndedAt,
		&summary.DurationMS, &summary.LLMCalls, &summary.ToolCalls, &summary.MCPCalls,
		&summary.LocalToolCalls, &summary.A2ACalls, &summary.RetrieverCalls, &summary.InputTokens,
		&summary.OutputTokens, &summary.TotalTokens, &summary.ErrorCount, &summary.RiskLevel,
		&summary.SpanCount, &summary.LastSpanAt, &summary.UpdatedAt, &sequence,
	)
	return summary, sequence, err
}

func (store *Store) GetTraceSummary(ctx context.Context, traceID string) (telemetry.Summary, error) {
	if !telemetry.ValidTraceID(traceID) {
		return telemetry.Summary{}, storage.ErrTraceNotFound
	}
	summary, _, err := scanTraceSummarySequence(store.pool.QueryRow(ctx, traceSummarySelect+` WHERE trace_id = $1`, traceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return telemetry.Summary{}, storage.ErrTraceNotFound
	}
	return summary, err
}

func (store *Store) GetTracePayload(ctx context.Context, traceID, spanID, kind string) (telemetry.Payload, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) || kind == "" {
		return telemetry.Payload{}, storage.ErrTraceNotFound
	}
	var payload telemetry.Payload
	err := store.pool.QueryRow(ctx, `
SELECT trace_id, span_id, payload_kind, content_type, encoding, payload_bytes,
       payload_json, redaction_state, size_bytes, expires_at, created_at
FROM trace_payloads
WHERE trace_id = $1 AND span_id = $2 AND payload_kind = $3
  AND (expires_at IS NULL OR expires_at > $4)
  AND redaction_state <> 'expired'
`, traceID, spanID, kind, store.options.Now().UTC()).Scan(
		&payload.TraceID, &payload.SpanID, &payload.Kind, &payload.ContentType, &payload.Encoding,
		&payload.PayloadBytes, &payload.PayloadJSON, &payload.RedactionState, &payload.SizeBytes,
		&payload.ExpiresAt, &payload.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return telemetry.Payload{}, storage.ErrTraceNotFound
	}
	return payload, err
}

func (store *Store) GetTracePayloads(ctx context.Context, traceID, spanID string) ([]telemetry.Payload, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return nil, storage.ErrTraceNotFound
	}
	now := store.options.Now().UTC()
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := getTraceSpan(ctx, transaction, traceID, spanID, false); errors.Is(err, pgx.ErrNoRows) {
		return nil, storage.ErrTraceNotFound
	} else if err != nil {
		return nil, err
	}
	payloads, err := getTracePayloads(ctx, transaction, traceID, spanID, now)
	if err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return payloads, nil
}

func (store *Store) GetTraceSpanDetail(ctx context.Context, traceID, spanID string) (storage.TraceSpanDetail, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return storage.TraceSpanDetail{}, storage.ErrTraceNotFound
	}
	now := store.options.Now().UTC()
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return storage.TraceSpanDetail{}, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	span, err := getTraceSpan(ctx, transaction, traceID, spanID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.TraceSpanDetail{}, storage.ErrTraceNotFound
	}
	if err != nil {
		return storage.TraceSpanDetail{}, err
	}
	if store.afterTraceSpanDetailSpanRead != nil {
		store.afterTraceSpanDetailSpanRead()
	}
	payloads, err := getTracePayloads(ctx, transaction, traceID, spanID, now)
	if err != nil {
		return storage.TraceSpanDetail{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return storage.TraceSpanDetail{}, err
	}
	return storage.TraceSpanDetail{Span: span, Payloads: payloads}, nil
}

func getTracePayloads(
	ctx context.Context,
	queryer traceReadQueryer,
	traceID string,
	spanID string,
	now time.Time,
) ([]telemetry.Payload, error) {
	rows, err := queryer.Query(ctx, `
SELECT trace_id, span_id, payload_kind, content_type, encoding, payload_bytes,
       payload_json, redaction_state, size_bytes, expires_at, created_at
FROM trace_payloads
WHERE trace_id = $1 AND span_id = $2
  AND (expires_at IS NULL OR expires_at > $3)
  AND redaction_state <> 'expired'
ORDER BY payload_kind
`, traceID, spanID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	payloads := []telemetry.Payload{}
	for rows.Next() {
		var payload telemetry.Payload
		if err := rows.Scan(
			&payload.TraceID, &payload.SpanID, &payload.Kind, &payload.ContentType, &payload.Encoding,
			&payload.PayloadBytes, &payload.PayloadJSON, &payload.RedactionState, &payload.SizeBytes,
			&payload.ExpiresAt, &payload.CreatedAt,
		); err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return payloads, nil
}

func nullableTraceBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func traceSpanKey(traceID, spanID string) string { return traceID + "\x00" + spanID }

var _ storage.TraceStore = (*Store)(nil)
