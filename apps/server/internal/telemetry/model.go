// Package telemetry contains the source-neutral Trace ingest model shared by
// the OTLP normalizer, persistence adapters, and summary assembler.
package telemetry

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type ContentMode string

const (
	ContentModeNone     ContentMode = "none"
	ContentModeMetadata ContentMode = "metadata"
	ContentModeFull     ContentMode = "full"
)

const (
	ContentStateCaptured     = "captured"
	ContentStateRedacted     = "redacted"
	ContentStateTruncated    = "truncated"
	ContentStateNotCollected = "not_collected"
	ContentStateExpired      = "expired"
)

const (
	StatusUnset = "unset"
	StatusOK    = "ok"
	StatusError = "error"
)

// Agentshark semantic attributes are explicit signals, not heuristics. The
// Python SDK and Collector share these keys.
const (
	AttributeAgentID       = "agentshark.agent.id"
	AttributeSessionID     = "agentshark.session.id"
	AttributeTaskID        = "agentshark.task.id"
	AttributeTaskRoot      = "agentshark.task.root"
	AttributeSpanKind      = "agentshark.span.kind"
	AttributeCountable     = "agentshark.countable"
	AttributeToolKind      = "agentshark.tool.kind"
	AttributeMCPServer     = "agentshark.mcp.server"
	AttributeMCPMethod     = "agentshark.mcp.method"
	AttributePeerAgentID   = "agentshark.peer_agent.id"
	AttributeInteractionID = "agentshark.interaction.id"
)

type Event struct {
	Name                   string         `json:"name"`
	Time                   time.Time      `json:"time"`
	Attributes             map[string]any `json:"attributes"`
	DroppedAttributesCount uint32         `json:"droppedAttributesCount,omitempty"`
}

type Span struct {
	TraceID                   string         `json:"traceId"`
	SpanID                    string         `json:"spanId"`
	ParentSpanID              string         `json:"parentSpanId,omitempty"`
	TraceState                string         `json:"traceState,omitempty"`
	Name                      string         `json:"name"`
	OpenInferenceKind         string         `json:"openInferenceKind,omitempty"`
	OTelSpanKind              int32          `json:"otelSpanKind"`
	StartedAt                 time.Time      `json:"startedAt"`
	EndedAt                   *time.Time     `json:"endedAt,omitempty"`
	DurationMS                *int64         `json:"durationMs,omitempty"`
	StatusCode                string         `json:"statusCode"`
	StatusMessage             string         `json:"statusMessage,omitempty"`
	AgentID                   string         `json:"agentId,omitempty"`
	SessionID                 string         `json:"sessionId,omitempty"`
	TaskID                    string         `json:"taskId,omitempty"`
	Provider                  string         `json:"provider,omitempty"`
	Model                     string         `json:"model,omitempty"`
	ToolName                  string         `json:"toolName,omitempty"`
	ToolKind                  string         `json:"toolKind,omitempty"`
	MCPServer                 string         `json:"mcpServer,omitempty"`
	PeerAgentID               string         `json:"peerAgentId,omitempty"`
	InputTokens               *int64         `json:"inputTokens,omitempty"`
	OutputTokens              *int64         `json:"outputTokens,omitempty"`
	TotalTokens               *int64         `json:"totalTokens,omitempty"`
	Countable                 bool           `json:"countable"`
	ContentState              string         `json:"contentState"`
	Attributes                map[string]any `json:"attributes"`
	Resource                  map[string]any `json:"resource"`
	Events                    []Event        `json:"events"`
	InstrumentationScope      string         `json:"instrumentationScope,omitempty"`
	InstrumentationVersion    string         `json:"instrumentationVersion,omitempty"`
	SemanticConventionVersion string         `json:"semanticConventionVersion,omitempty"`
	ReceivedAt                time.Time      `json:"receivedAt"`
	UpdatedAt                 time.Time      `json:"updatedAt"`
}

type Link struct {
	TraceID       string         `json:"traceId"`
	SpanID        string         `json:"spanId"`
	LinkedTraceID string         `json:"linkedTraceId"`
	LinkedSpanID  string         `json:"linkedSpanId"`
	Attributes    map[string]any `json:"attributes"`
}

type Payload struct {
	TraceID        string          `json:"traceId"`
	SpanID         string          `json:"spanId"`
	Kind           string          `json:"kind"`
	ContentType    string          `json:"contentType"`
	Encoding       string          `json:"encoding"`
	PayloadBytes   []byte          `json:"payloadBytes,omitempty"`
	PayloadJSON    json.RawMessage `json:"payloadJson,omitempty"`
	RedactionState string          `json:"redactionState"`
	SizeBytes      int64           `json:"sizeBytes"`
	ExpiresAt      *time.Time      `json:"expiresAt,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type TraceBatch struct {
	Spans    []Span    `json:"spans"`
	Links    []Link    `json:"links"`
	Payloads []Payload `json:"payloads"`
}

type WriteResult struct {
	Inserted   int      `json:"inserted"`
	Updated    int      `json:"updated"`
	Duplicates int      `json:"duplicates"`
	TraceIDs   []string `json:"traceIds"`
}

type Summary struct {
	TraceID        string     `json:"traceId"`
	TaskID         string     `json:"taskId,omitempty"`
	SessionID      string     `json:"sessionId,omitempty"`
	RootAgentID    string     `json:"rootAgentId,omitempty"`
	RootSpanID     string     `json:"rootSpanId,omitempty"`
	Status         string     `json:"status"`
	Completeness   string     `json:"completeness"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	DurationMS     *int64     `json:"durationMs,omitempty"`
	LLMCalls       int        `json:"llmCalls"`
	ToolCalls      int        `json:"toolCalls"`
	MCPCalls       int        `json:"mcpCalls"`
	LocalToolCalls int        `json:"localToolCalls"`
	A2ACalls       int        `json:"a2aCalls"`
	RetrieverCalls int        `json:"retrieverCalls"`
	InputTokens    int64      `json:"inputTokens"`
	OutputTokens   int64      `json:"outputTokens"`
	TotalTokens    int64      `json:"totalTokens"`
	ErrorCount     int        `json:"errorCount"`
	RiskLevel      string     `json:"riskLevel,omitempty"`
	SpanCount      int        `json:"spanCount"`
	LastSpanAt     time.Time  `json:"lastSpanAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func (mode ContentMode) Valid() bool {
	return mode == ContentModeNone || mode == ContentModeMetadata || mode == ContentModeFull
}

func ValidTraceID(value string) bool { return validHexID(value, 16) }

func ValidSpanID(value string) bool { return validHexID(value, 8) }

func validHexID(value string, byteLength int) bool {
	if len(value) != byteLength*2 || strings.Trim(value, "0") == "" {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == byteLength
}

// PrepareSpan applies the storage precision and validates invariants again at
// the persistence boundary. Collector validation is not a substitute for this
// check because tests and future importers can call a Store directly.
func PrepareSpan(span Span, now time.Time) (Span, error) {
	span.TraceID = strings.ToLower(strings.TrimSpace(span.TraceID))
	span.SpanID = strings.ToLower(strings.TrimSpace(span.SpanID))
	span.ParentSpanID = strings.ToLower(strings.TrimSpace(span.ParentSpanID))
	span.Name = strings.TrimSpace(span.Name)
	if !ValidTraceID(span.TraceID) {
		return Span{}, errors.New("trace ID must be a non-zero 16-byte hexadecimal value")
	}
	if !ValidSpanID(span.SpanID) {
		return Span{}, errors.New("span ID must be a non-zero 8-byte hexadecimal value")
	}
	if span.ParentSpanID != "" && !ValidSpanID(span.ParentSpanID) {
		return Span{}, errors.New("parent span ID must be empty or a non-zero 8-byte hexadecimal value")
	}
	if span.Name == "" {
		return Span{}, errors.New("span name is required")
	}
	if span.StartedAt.IsZero() {
		return Span{}, errors.New("span start time is required")
	}
	span.StartedAt = span.StartedAt.UTC().Truncate(time.Microsecond)
	if span.EndedAt != nil {
		endedAt := span.EndedAt.UTC().Truncate(time.Microsecond)
		if endedAt.Before(span.StartedAt) {
			return Span{}, errors.New("span end time precedes start time")
		}
		span.EndedAt = &endedAt
		duration := endedAt.Sub(span.StartedAt).Milliseconds()
		span.DurationMS = &duration
	} else {
		span.DurationMS = nil
	}
	if span.StatusCode == "" {
		span.StatusCode = StatusUnset
	}
	if span.ContentState == "" {
		span.ContentState = ContentStateNotCollected
	}
	if !validContentState(span.ContentState, true) {
		return Span{}, errors.New("trace span content state is invalid")
	}
	for label, value := range map[string]*int64{
		"input tokens": span.InputTokens, "output tokens": span.OutputTokens, "total tokens": span.TotalTokens,
	} {
		if value != nil && *value < 0 {
			return Span{}, fmt.Errorf("%s cannot be negative", label)
		}
	}
	if span.Attributes == nil {
		span.Attributes = map[string]any{}
	}
	if span.Resource == nil {
		span.Resource = map[string]any{}
	}
	if span.Events == nil {
		span.Events = []Event{}
	}
	for index := range span.Events {
		span.Events[index].Time = span.Events[index].Time.UTC()
		if span.Events[index].Attributes == nil {
			span.Events[index].Attributes = map[string]any{}
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC().Truncate(time.Microsecond)
	if span.ReceivedAt.IsZero() {
		span.ReceivedAt = now
	} else {
		span.ReceivedAt = span.ReceivedAt.UTC().Truncate(time.Microsecond)
	}
	span.UpdatedAt = now
	return span, nil
}

func PrepareLink(link Link) (Link, error) {
	link.TraceID = strings.ToLower(strings.TrimSpace(link.TraceID))
	link.SpanID = strings.ToLower(strings.TrimSpace(link.SpanID))
	link.LinkedTraceID = strings.ToLower(strings.TrimSpace(link.LinkedTraceID))
	link.LinkedSpanID = strings.ToLower(strings.TrimSpace(link.LinkedSpanID))
	if !ValidTraceID(link.TraceID) || !ValidSpanID(link.SpanID) ||
		!ValidTraceID(link.LinkedTraceID) || !ValidSpanID(link.LinkedSpanID) {
		return Link{}, errors.New("trace link contains an invalid trace or span ID")
	}
	if link.Attributes == nil {
		link.Attributes = map[string]any{}
	}
	return link, nil
}

func PreparePayload(payload Payload, now time.Time) (Payload, error) {
	payload.TraceID = strings.ToLower(strings.TrimSpace(payload.TraceID))
	payload.SpanID = strings.ToLower(strings.TrimSpace(payload.SpanID))
	payload.Kind = strings.TrimSpace(payload.Kind)
	if !ValidTraceID(payload.TraceID) || !ValidSpanID(payload.SpanID) {
		return Payload{}, errors.New("trace payload contains an invalid trace or span ID")
	}
	if payload.Kind == "" {
		return Payload{}, errors.New("trace payload kind is required")
	}
	if (len(payload.PayloadBytes) == 0) == (len(payload.PayloadJSON) == 0) {
		return Payload{}, errors.New("trace payload must contain exactly one bytes or JSON value")
	}
	if len(payload.PayloadJSON) > 0 && !json.Valid(payload.PayloadJSON) {
		return Payload{}, errors.New("trace payload JSON is invalid")
	}
	storedSize := len(payload.PayloadBytes) + len(payload.PayloadJSON)
	if payload.SizeBytes < int64(storedSize) {
		return Payload{}, errors.New("trace payload size is smaller than its stored content")
	}
	if payload.ContentType == "" {
		payload.ContentType = "application/json"
	}
	if payload.Encoding == "" {
		payload.Encoding = "identity"
	}
	if payload.RedactionState == "" {
		payload.RedactionState = ContentStateCaptured
	}
	if !validContentState(payload.RedactionState, false) || payload.RedactionState == ContentStateNotCollected {
		return Payload{}, errors.New("trace payload redaction state is invalid")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if payload.CreatedAt.IsZero() {
		payload.CreatedAt = now.UTC().Truncate(time.Microsecond)
	} else {
		payload.CreatedAt = payload.CreatedAt.UTC().Truncate(time.Microsecond)
	}
	if payload.ExpiresAt != nil {
		expiresAt := payload.ExpiresAt.UTC().Truncate(time.Microsecond)
		payload.ExpiresAt = &expiresAt
	}
	payload.PayloadBytes = append([]byte(nil), payload.PayloadBytes...)
	payload.PayloadJSON = append(json.RawMessage(nil), payload.PayloadJSON...)
	return payload, nil
}

func validContentState(value string, allowExpired bool) bool {
	switch value {
	case ContentStateCaptured, ContentStateRedacted, ContentStateTruncated, ContentStateNotCollected:
		return true
	case ContentStateExpired:
		return allowExpired
	default:
		return false
	}
}

// MergeSpan prevents a late retry of an unfinished span from replacing an
// already persisted terminal record. Terminal updates at the same or later end
// time remain allowed so status, tokens, events, and payload state can converge.
func MergeSpan(existing, incoming Span) Span {
	if existing.EndedAt != nil && (incoming.EndedAt == nil || incoming.EndedAt.Before(*existing.EndedAt)) {
		return existing
	}
	if existing.ContentState == ContentStateExpired {
		incoming.ContentState = ContentStateExpired
	}
	incoming.ReceivedAt = existing.ReceivedAt
	return incoming
}

func EqualSpan(left, right Span) bool {
	left.ReceivedAt, right.ReceivedAt = time.Time{}, time.Time{}
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftJSON, rightJSON)
}

func SortedTraceIDs(values map[string]struct{}) []string {
	ids := make([]string, 0, len(values))
	for value := range values {
		ids = append(ids, value)
	}
	sort.Strings(ids)
	return ids
}
