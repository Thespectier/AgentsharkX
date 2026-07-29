CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigserial NOT NULL UNIQUE,
    source text NOT NULL,
    public_id text NOT NULL,
    upstream_id text NOT NULL,
    event_type text NOT NULL,
    severity text,
    status text,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    trace_id text,
    span_id text,
    interaction_id text,
    agent_id text,
    session_id text,
    task_id text,
    summary_json jsonb NOT NULL,
    has_payload boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, upstream_id),
    UNIQUE (source, public_id)
);

CREATE INDEX audit_events_occurred_at_idx ON audit_events (occurred_at DESC, id DESC);
CREATE INDEX audit_events_source_occurred_at_idx ON audit_events (source, occurred_at DESC);
CREATE INDEX audit_events_type_occurred_at_idx ON audit_events (event_type, occurred_at DESC);
CREATE INDEX audit_events_severity_occurred_at_idx ON audit_events (severity, occurred_at DESC);
CREATE INDEX audit_events_trace_id_idx ON audit_events (trace_id) WHERE trace_id IS NOT NULL;

CREATE TABLE audit_payloads (
    event_id uuid PRIMARY KEY REFERENCES audit_events(id) ON DELETE CASCADE,
    content_type text NOT NULL,
    encoding text NOT NULL,
    payload_bytes bytea,
    payload_json jsonb,
    redaction_state text NOT NULL,
    size_bytes bigint NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((payload_bytes IS NULL) <> (payload_json IS NULL))
);

CREATE INDEX audit_payloads_expires_at_idx ON audit_payloads (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE stream_outbox (
    sequence bigserial PRIMARY KEY,
    topic text NOT NULL,
    entity_id text NOT NULL,
    event_kind text NOT NULL,
    event_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz
);

CREATE INDEX stream_outbox_expires_at_idx ON stream_outbox (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE stream_outbox_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    latest_sequence bigint NOT NULL DEFAULT 0 CHECK (latest_sequence >= 0)
);

INSERT INTO stream_outbox_state (singleton, latest_sequence) VALUES (true, 0);

CREATE TABLE ingest_checkpoints (
    source text PRIMARY KEY,
    cursor_json jsonb NOT NULL,
    last_success_at timestamptz,
    last_attempt_at timestamptz,
    last_error text,
    updated_at timestamptz NOT NULL DEFAULT now()
);
