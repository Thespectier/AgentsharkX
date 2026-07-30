CREATE TABLE demo_runs (
    run_id uuid PRIMARY KEY,
    list_sequence bigserial NOT NULL UNIQUE,
    scenario text NOT NULL CHECK (scenario IN ('happy', 'approval', 'failure')),
    status text NOT NULL CHECK (status IN ('queued', 'starting', 'running', 'waiting_approval', 'succeeded', 'failed', 'cancelled', 'interrupted', 'expired')),
    outcome text NOT NULL CHECK (outcome IN ('none', 'normal', 'approved', 'denied', 'degraded', 'cancelled', 'failed')),
    status_reason_code text,
    requested_at timestamptz NOT NULL,
    started_at timestamptz,
    completed_at timestamptz,
    last_heartbeat_at timestamptz,
    run_version bigint NOT NULL DEFAULT 0 CHECK (run_version >= 0),
    delay_ms integer NOT NULL CHECK (delay_ms BETWEEN 0 AND 2000),
    task_id text NOT NULL UNIQUE,
    session_id text NOT NULL UNIQUE,
    trace_id text,
    root_span_id text,
    root_agent_id text NOT NULL,
    approval_ticket_id text,
    approval_status text,
    approval_json jsonb,
    current_step text,
    completed_steps integer NOT NULL DEFAULT 0 CHECK (completed_steps >= 0),
    total_steps integer NOT NULL CHECK (total_steps >= 0),
    fixture_version text NOT NULL,
    request_id text NOT NULL UNIQUE,
    error_code text,
    error_summary text CHECK (error_summary IS NULL OR length(error_summary) <= 500),
    created_by text,
    observed_metrics_json jsonb,
    CHECK (completed_steps <= total_steps),
    CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$'),
    CHECK (root_span_id IS NULL OR root_span_id ~ '^[0-9a-f]{16}$')
);

CREATE INDEX demo_runs_trace_id_idx ON demo_runs (trace_id) WHERE trace_id IS NOT NULL;
CREATE INDEX demo_runs_list_sequence_idx ON demo_runs (list_sequence DESC);
CREATE UNIQUE INDEX demo_runs_single_active ON demo_runs ((1))
    WHERE status IN ('queued', 'starting', 'running', 'waiting_approval');

CREATE INDEX stream_outbox_demo_run_idx
    ON stream_outbox (entity_id, sequence)
    WHERE topic = 'demo.run';
