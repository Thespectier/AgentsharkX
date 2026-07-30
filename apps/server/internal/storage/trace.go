package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

var (
	ErrTraceNotFound      = errors.New("stored trace record was not found")
	ErrInvalidTraceCursor = errors.New("invalid trace cursor")
)

const (
	DefaultTraceGraphSpanLimit = 1000
	DefaultTraceGraphLinkLimit = 4000
	MaxTraceGraphSpanLimit     = 5000
	MaxTraceGraphLinkLimit     = 20000
)

type TraceFilter struct {
	Cursor        string
	Limit         int
	Status        string
	Completeness  string
	AgentID       string
	SessionID     string
	TaskID        string
	HasError      *bool
	HasA2A        *bool
	StartedAfter  *time.Time
	StartedBefore *time.Time
	Query         string
}

type TraceGraphLimits struct {
	SpanLimit int
	LinkLimit int
}

type TraceGraph struct {
	Spans          []telemetry.Span
	Links          []telemetry.Link
	TotalSpans     int
	TotalLinks     int
	SpansTruncated bool
	LinksTruncated bool
}

type TraceDetail struct {
	Summary  telemetry.Summary
	Graph    TraceGraph
	RootSpan *telemetry.Span
}

type TraceSpanDetail struct {
	Span     telemetry.Span
	Payloads []telemetry.Payload
}

// TraceWriter is the Collector's minimal ingest capability. Production grants
// it Trace-table merge and summary-outbox writes independently of BFF reads.
type TraceWriter interface {
	WriteBatch(context.Context, telemetry.TraceBatch) (telemetry.WriteResult, error)
}

type TraceMaintainer interface {
	PruneTraces(context.Context, time.Time) error
}

type TraceReader interface {
	ListTraceSummaries(context.Context, TraceFilter) (Page[telemetry.Summary], error)
	GetTraceSpans(context.Context, string) ([]telemetry.Span, error)
	GetTraceLinks(context.Context, string) ([]telemetry.Link, error)
	GetTraceSummary(context.Context, string) (telemetry.Summary, error)
	GetTraceGraph(context.Context, string, TraceGraphLimits) (TraceGraph, error)
	GetTraceDetail(context.Context, string, TraceGraphLimits) (TraceDetail, error)
	GetTraceSpan(context.Context, string, string) (telemetry.Span, error)
	GetTracePayload(context.Context, string, string, string) (telemetry.Payload, error)
	GetTracePayloads(context.Context, string, string) ([]telemetry.Payload, error)
	GetTraceSpanDetail(context.Context, string, string) (TraceSpanDetail, error)
}

type TraceStore interface {
	TraceWriter
	TraceReader
	TraceMaintainer
}

type traceCursor struct {
	Version   int    `json:"v"`
	Watermark int64  `json:"watermark"`
	Sequence  int64  `json:"sequence"`
	Filter    string `json:"filter"`
}

type traceFilterIdentity struct {
	Status        string     `json:"status,omitempty"`
	Completeness  string     `json:"completeness,omitempty"`
	AgentID       string     `json:"agentId,omitempty"`
	SessionID     string     `json:"sessionId,omitempty"`
	TaskID        string     `json:"taskId,omitempty"`
	HasError      *bool      `json:"hasError,omitempty"`
	HasA2A        *bool      `json:"hasA2A,omitempty"`
	StartedAfter  *time.Time `json:"startedAfter,omitempty"`
	StartedBefore *time.Time `json:"startedBefore,omitempty"`
	Query         string     `json:"query,omitempty"`
}

func NormalizeTraceFilter(filter TraceFilter) TraceFilter {
	filter.Cursor = strings.TrimSpace(filter.Cursor)
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	filter.Completeness = strings.ToLower(strings.TrimSpace(filter.Completeness))
	filter.AgentID = strings.TrimSpace(filter.AgentID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.TaskID = strings.TrimSpace(filter.TaskID)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.StartedAfter = utcTimePointer(filter.StartedAfter)
	filter.StartedBefore = utcTimePointer(filter.StartedBefore)
	return filter
}

func NormalizeTraceGraphLimits(limits TraceGraphLimits) TraceGraphLimits {
	if limits.SpanLimit < 1 {
		limits.SpanLimit = DefaultTraceGraphSpanLimit
	} else if limits.SpanLimit > MaxTraceGraphSpanLimit {
		limits.SpanLimit = MaxTraceGraphSpanLimit
	}
	if limits.LinkLimit < 1 {
		limits.LinkLimit = DefaultTraceGraphLinkLimit
	} else if limits.LinkLimit > MaxTraceGraphLinkLimit {
		limits.LinkLimit = MaxTraceGraphLinkLimit
	}
	return limits
}

func EncodeTraceCursor(watermark, sequence int64, filter TraceFilter) (string, error) {
	fingerprint, err := traceFilterFingerprint(filter)
	if err != nil {
		return "", err
	}
	document, err := json.Marshal(traceCursor{Version: 1, Watermark: watermark, Sequence: sequence, Filter: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(document), nil
}

func DecodeTraceCursor(value string, filter TraceFilter) (int64, int64, error) {
	document, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return 0, 0, ErrInvalidTraceCursor
	}
	var cursor traceCursor
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, 0, ErrInvalidTraceCursor
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, 0, ErrInvalidTraceCursor
	}
	fingerprint, err := traceFilterFingerprint(filter)
	if err != nil || cursor.Version != 1 || cursor.Watermark < 1 || cursor.Sequence < 1 ||
		cursor.Sequence > cursor.Watermark || cursor.Filter != fingerprint {
		return 0, 0, ErrInvalidTraceCursor
	}
	return cursor.Watermark, cursor.Sequence, nil
}

func traceFilterFingerprint(filter TraceFilter) (string, error) {
	filter = NormalizeTraceFilter(filter)
	document, err := json.Marshal(traceFilterIdentity{
		Status: filter.Status, Completeness: filter.Completeness, AgentID: filter.AgentID,
		SessionID: filter.SessionID, TaskID: filter.TaskID, HasError: cloneBool(filter.HasError),
		HasA2A: cloneBool(filter.HasA2A), StartedAfter: filter.StartedAfter,
		StartedBefore: filter.StartedBefore, Query: strings.ToLower(filter.Query),
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:]), nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
