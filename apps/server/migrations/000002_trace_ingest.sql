CREATE TABLE trace_spans (
    trace_id text NOT NULL,
    span_id text NOT NULL,
    parent_span_id text,
    trace_state text,
    span_name text NOT NULL,
    openinference_kind text,
    otel_span_kind smallint,
    started_at timestamptz NOT NULL,
    ended_at timestamptz,
    duration_ms bigint,
    status_code text NOT NULL,
    status_message text,
    agent_id text,
    session_id text,
    task_id text,
    provider text,
    model text,
    tool_name text,
    tool_kind text,
    mcp_server text,
    peer_agent_id text,
    input_tokens bigint,
    output_tokens bigint,
    total_tokens bigint,
    countable boolean NOT NULL DEFAULT false,
    content_state text NOT NULL,
    attributes_json jsonb NOT NULL,
    resource_json jsonb NOT NULL,
    events_json jsonb NOT NULL,
    instrumentation_scope text,
    instrumentation_version text,
    semantic_convention_version text,
    received_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (trace_id, span_id),
    CHECK (content_state IN ('captured', 'redacted', 'truncated', 'not_collected', 'expired')),
    CHECK (ended_at IS NULL OR ended_at >= started_at),
    CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CHECK (input_tokens IS NULL OR input_tokens >= 0),
    CHECK (output_tokens IS NULL OR output_tokens >= 0),
    CHECK (total_tokens IS NULL OR total_tokens >= 0)
);

CREATE INDEX trace_spans_started_at_idx ON trace_spans (started_at DESC, trace_id);
CREATE INDEX trace_spans_trace_tree_idx ON trace_spans (trace_id, started_at, span_id);
CREATE INDEX trace_spans_task_id_idx ON trace_spans (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX trace_spans_session_id_idx ON trace_spans (session_id) WHERE session_id IS NOT NULL;
CREATE INDEX trace_spans_agent_id_idx ON trace_spans (agent_id) WHERE agent_id IS NOT NULL;
CREATE INDEX trace_spans_kind_started_at_idx ON trace_spans (openinference_kind, started_at DESC);

CREATE TABLE trace_links (
    trace_id text NOT NULL,
    span_id text NOT NULL,
    linked_trace_id text NOT NULL,
    linked_span_id text NOT NULL,
    attributes_json jsonb NOT NULL,
    PRIMARY KEY (trace_id, span_id, linked_trace_id, linked_span_id),
    FOREIGN KEY (trace_id, span_id) REFERENCES trace_spans(trace_id, span_id) ON DELETE CASCADE
);

CREATE TABLE trace_summaries (
    trace_id text PRIMARY KEY,
    task_id text,
    session_id text,
    root_agent_id text,
    root_span_id text,
    status text NOT NULL,
    completeness text NOT NULL,
    started_at timestamptz NOT NULL,
    ended_at timestamptz,
    duration_ms bigint,
    llm_calls integer NOT NULL DEFAULT 0,
    tool_calls integer NOT NULL DEFAULT 0,
    mcp_calls integer NOT NULL DEFAULT 0,
    local_tool_calls integer NOT NULL DEFAULT 0,
    a2a_calls integer NOT NULL DEFAULT 0,
    retriever_calls integer NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    total_tokens bigint NOT NULL DEFAULT 0,
    error_count integer NOT NULL DEFAULT 0,
    risk_level text,
    span_count integer NOT NULL DEFAULT 0,
    last_span_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (ended_at IS NULL OR ended_at >= started_at),
    CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CHECK (llm_calls >= 0 AND tool_calls >= 0 AND mcp_calls >= 0 AND local_tool_calls >= 0),
    CHECK (a2a_calls >= 0 AND retriever_calls >= 0 AND error_count >= 0 AND span_count >= 0),
    CHECK (input_tokens >= 0 AND output_tokens >= 0 AND total_tokens >= 0)
);

CREATE INDEX trace_summaries_started_at_idx ON trace_summaries (started_at DESC, trace_id);
CREATE INDEX trace_summaries_task_id_idx ON trace_summaries (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX trace_summaries_session_id_idx ON trace_summaries (session_id) WHERE session_id IS NOT NULL;

CREATE TABLE trace_payloads (
    trace_id text NOT NULL,
    span_id text NOT NULL,
    payload_kind text NOT NULL,
    content_type text NOT NULL,
    encoding text NOT NULL,
    payload_bytes bytea,
    payload_json jsonb,
    redaction_state text NOT NULL,
    size_bytes bigint NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (trace_id, span_id, payload_kind),
    FOREIGN KEY (trace_id, span_id) REFERENCES trace_spans(trace_id, span_id) ON DELETE CASCADE,
    CHECK (
        (redaction_state = 'expired' AND payload_bytes IS NULL AND payload_json IS NULL)
        OR
        (redaction_state <> 'expired' AND ((payload_bytes IS NULL) <> (payload_json IS NULL)))
    ),
    CHECK (redaction_state IN ('captured', 'redacted', 'truncated', 'expired')),
    CHECK (size_bytes >= 0)
);

CREATE INDEX trace_payloads_expires_at_idx ON trace_payloads (expires_at) WHERE expires_at IS NOT NULL;
