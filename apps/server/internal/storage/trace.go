package storage

import (
	"context"
	"errors"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

var ErrTraceNotFound = errors.New("stored trace record was not found")

// TraceWriter is the Collector's minimal database capability. It can be wired
// with a write-only database account independently of BFF Trace reads.
type TraceWriter interface {
	WriteBatch(context.Context, telemetry.TraceBatch) (telemetry.WriteResult, error)
}

type TraceMaintainer interface {
	PruneTraces(context.Context, time.Time) error
}

type TraceReader interface {
	GetTraceSpans(context.Context, string) ([]telemetry.Span, error)
	GetTraceLinks(context.Context, string) ([]telemetry.Link, error)
	GetTraceSummary(context.Context, string) (telemetry.Summary, error)
	GetTracePayload(context.Context, string, string, string) (telemetry.Payload, error)
}

type TraceStore interface {
	TraceWriter
	TraceReader
	TraceMaintainer
}
