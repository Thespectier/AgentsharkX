package postgres

import (
	"context"
	"encoding/json"
	"errors"

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
	}
	if err := transaction.Commit(ctx); err != nil {
		return telemetry.WriteResult{}, err
	}
	return result, nil
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

func getTraceSpan(ctx context.Context, transaction pgx.Tx, traceID, spanID string, lock bool) (telemetry.Span, error) {
	query := traceSpanSelect + ` WHERE trace_id = $1 AND span_id = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanTraceSpan(transaction.QueryRow(ctx, query, traceID, spanID))
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

func (store *Store) GetTraceSummary(ctx context.Context, traceID string) (telemetry.Summary, error) {
	if !telemetry.ValidTraceID(traceID) {
		return telemetry.Summary{}, storage.ErrTraceNotFound
	}
	var summary telemetry.Summary
	err := store.pool.QueryRow(ctx, `
SELECT trace_id, COALESCE(task_id, ''), COALESCE(session_id, ''), COALESCE(root_agent_id, ''),
       COALESCE(root_span_id, ''), status, completeness, started_at, ended_at, duration_ms,
       llm_calls, tool_calls, mcp_calls, local_tool_calls, a2a_calls, retriever_calls,
       input_tokens, output_tokens, total_tokens, error_count, COALESCE(risk_level, ''),
       span_count, last_span_at, updated_at
FROM trace_summaries WHERE trace_id = $1
`, traceID).Scan(
		&summary.TraceID, &summary.TaskID, &summary.SessionID, &summary.RootAgentID, &summary.RootSpanID,
		&summary.Status, &summary.Completeness, &summary.StartedAt, &summary.EndedAt,
		&summary.DurationMS, &summary.LLMCalls, &summary.ToolCalls, &summary.MCPCalls,
		&summary.LocalToolCalls, &summary.A2ACalls, &summary.RetrieverCalls, &summary.InputTokens,
		&summary.OutputTokens, &summary.TotalTokens, &summary.ErrorCount, &summary.RiskLevel,
		&summary.SpanCount, &summary.LastSpanAt, &summary.UpdatedAt,
	)
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

func traceSpanKey(traceID, spanID string) string { return traceID + "\x00" + spanID }

var _ storage.TraceStore = (*Store)(nil)
