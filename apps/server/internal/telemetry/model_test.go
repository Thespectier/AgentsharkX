package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPrepareSpanRejectsUnknownContentState(t *testing.T) {
	t.Parallel()
	_, err := PrepareSpan(Span{
		TraceID:      "11111111111111111111111111111111",
		SpanID:       "2222222222222222",
		Name:         "task",
		StartedAt:    time.Unix(1, 0),
		ContentState: "available",
	}, time.Unix(2, 0))
	if err == nil || !strings.Contains(err.Error(), "content state") {
		t.Fatalf("PrepareSpan() error = %v", err)
	}
}

func TestPreparePayloadRejectsUnknownRedactionState(t *testing.T) {
	t.Parallel()
	_, err := PreparePayload(Payload{
		TraceID:        "11111111111111111111111111111111",
		SpanID:         "2222222222222222",
		Kind:           "input",
		PayloadJSON:    json.RawMessage(`{"prompt":"hello"}`),
		RedactionState: "available",
		SizeBytes:      18,
	}, time.Unix(2, 0))
	if err == nil || !strings.Contains(err.Error(), "redaction state") {
		t.Fatalf("PreparePayload() error = %v", err)
	}
}
