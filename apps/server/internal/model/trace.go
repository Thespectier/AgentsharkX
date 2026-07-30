package model

import (
	"encoding/json"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

// TraceSummary is the payload-free, persisted projection assembled from one
// explicitly identified OTLP Trace.
type TraceSummary = telemetry.Summary

// TraceSpan is the payload-free graph node returned by Trace detail. Raw
// attributes, resource fields, events, and retained content are available only
// from the authenticated single-Span endpoint.
type TraceSpan struct {
	TraceID                   string     `json:"traceId"`
	SpanID                    string     `json:"spanId"`
	ParentSpanID              string     `json:"parentSpanId,omitempty"`
	TraceState                string     `json:"traceState,omitempty"`
	Name                      string     `json:"name"`
	OpenInferenceKind         string     `json:"openInferenceKind,omitempty"`
	OTelSpanKind              int32      `json:"otelSpanKind"`
	StartedAt                 time.Time  `json:"startedAt"`
	EndedAt                   *time.Time `json:"endedAt"`
	DurationMS                *int64     `json:"durationMs"`
	StatusCode                string     `json:"statusCode"`
	AgentID                   string     `json:"agentId,omitempty"`
	SessionID                 string     `json:"sessionId,omitempty"`
	TaskID                    string     `json:"taskId,omitempty"`
	Provider                  string     `json:"provider,omitempty"`
	Model                     string     `json:"model,omitempty"`
	ToolName                  string     `json:"toolName,omitempty"`
	ToolKind                  string     `json:"toolKind,omitempty"`
	MCPServer                 string     `json:"mcpServer,omitempty"`
	PeerAgentID               string     `json:"peerAgentId,omitempty"`
	InputTokens               *int64     `json:"inputTokens"`
	OutputTokens              *int64     `json:"outputTokens"`
	TotalTokens               *int64     `json:"totalTokens"`
	Countable                 bool       `json:"countable"`
	ContentState              string     `json:"contentState"`
	InstrumentationScope      string     `json:"instrumentationScope,omitempty"`
	InstrumentationVersion    string     `json:"instrumentationVersion,omitempty"`
	SemanticConventionVersion string     `json:"semanticConventionVersion,omitempty"`
	ReceivedAt                time.Time  `json:"receivedAt"`
	UpdatedAt                 time.Time  `json:"updatedAt"`
}

type TraceLink struct {
	TraceID       string `json:"traceId"`
	SpanID        string `json:"spanId"`
	LinkedTraceID string `json:"linkedTraceId"`
	LinkedSpanID  string `json:"linkedSpanId"`
}

type TraceCoverage struct {
	Source                string   `json:"source"`
	AgentIDs              []string `json:"agentIds"`
	PeerAgentIDs          []string `json:"peerAgentIds"`
	Providers             []string `json:"providers"`
	Models                []string `json:"models"`
	MCPServers            []string `json:"mcpServers"`
	SpanKinds             []string `json:"spanKinds"`
	InstrumentationScopes []string `json:"instrumentationScopes"`
	ContentStates         []string `json:"contentStates"`
}

type TraceListPage struct {
	Items      []TraceSummary `json:"items"`
	NextCursor *string        `json:"nextCursor"`
	Total      int            `json:"total"`
}

type TraceListEnvelope struct {
	Data TraceListPage `json:"data"`
	Meta Meta          `json:"meta"`
}

type TraceDetail struct {
	Summary        TraceSummary  `json:"summary"`
	RootSpan       *TraceSpan    `json:"rootSpan,omitempty"`
	Spans          []TraceSpan   `json:"spans"`
	Links          []TraceLink   `json:"links"`
	Coverage       TraceCoverage `json:"coverage"`
	TotalSpans     int           `json:"totalSpans"`
	TotalLinks     int           `json:"totalLinks"`
	SpansTruncated bool          `json:"spansTruncated"`
	LinksTruncated bool          `json:"linksTruncated"`
}

type TraceDetailEnvelope struct {
	Data TraceDetail `json:"data"`
	Meta Meta        `json:"meta"`
}

type TraceEvent struct {
	Name                   string         `json:"name"`
	Time                   time.Time      `json:"time"`
	Attributes             map[string]any `json:"attributes"`
	DroppedAttributesCount uint32         `json:"droppedAttributesCount"`
}

type TracePayload struct {
	Kind           string          `json:"kind"`
	ContentType    string          `json:"contentType"`
	Encoding       string          `json:"encoding"`
	PayloadBytes   []byte          `json:"payloadBytes,omitempty"`
	PayloadJSON    json.RawMessage `json:"payloadJson,omitempty"`
	RedactionState string          `json:"redactionState"`
	SizeBytes      int64           `json:"sizeBytes"`
	ExpiresAt      *time.Time      `json:"expiresAt"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type TraceSpanDetail struct {
	Span          TraceSpan      `json:"span"`
	StatusMessage string         `json:"statusMessage,omitempty"`
	Attributes    map[string]any `json:"attributes"`
	Resource      map[string]any `json:"resource"`
	Events        []TraceEvent   `json:"events"`
	Payloads      []TracePayload `json:"payloads"`
}

type TraceSpanDetailEnvelope struct {
	Data TraceSpanDetail `json:"data"`
	Meta Meta            `json:"meta"`
}
