ALTER TABLE trace_summaries
    ADD COLUMN list_sequence bigserial;

CREATE UNIQUE INDEX trace_summaries_list_sequence_idx
    ON trace_summaries (list_sequence);

CREATE INDEX trace_summaries_root_agent_list_idx
    ON trace_summaries (root_agent_id, list_sequence DESC)
    WHERE root_agent_id IS NOT NULL;

CREATE INDEX trace_summaries_status_list_idx
    ON trace_summaries (status, list_sequence DESC);

CREATE INDEX trace_summaries_completeness_list_idx
    ON trace_summaries (completeness, list_sequence DESC);

CREATE INDEX trace_summaries_task_list_idx
    ON trace_summaries (task_id, list_sequence DESC)
    WHERE task_id IS NOT NULL;

CREATE INDEX trace_summaries_session_list_idx
    ON trace_summaries (session_id, list_sequence DESC)
    WHERE session_id IS NOT NULL;

CREATE INDEX trace_summaries_error_list_idx
    ON trace_summaries (list_sequence DESC)
    WHERE error_count > 0;

CREATE INDEX trace_summaries_a2a_list_idx
    ON trace_summaries (list_sequence DESC)
    WHERE a2a_calls > 0;
