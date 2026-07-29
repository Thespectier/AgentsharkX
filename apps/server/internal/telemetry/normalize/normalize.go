// Package normalize converts real OTLP protobuf records into the stable
// AgentsharkX Trace persistence model.
package normalize

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

const (
	defaultPayloadLimit     = int64(256 * 1024)
	defaultPayloadRetention = 24 * time.Hour
	defaultTraceRetention   = 30 * 24 * time.Hour
)

var credentialValuePattern = regexp.MustCompile(
	`(?i)(?:authorization|api[-_ ]?key|password|secret|cookie|credentials?|session[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|client[-_ ]?secret)["']?\s*[:=]|bearer\s+\S+`,
)

type Options struct {
	ContentMode      telemetry.ContentMode
	PayloadLimit     int64
	PayloadRetention time.Duration
	TraceRetention   time.Duration
	Now              func() time.Time
}

type Rejection struct {
	TraceID string `json:"traceId,omitempty"`
	SpanID  string `json:"spanId,omitempty"`
	Reason  string `json:"reason"`
}

type Report struct {
	Received   int         `json:"received"`
	Accepted   int         `json:"accepted"`
	Rejections []Rejection `json:"rejections"`
}

func (report Report) Rejected() int { return len(report.Rejections) }

// Traces normalizes every ResourceSpans entry independently. A malformed span
// is rejected without preventing valid siblings in the same OTLP request from
// being persisted.
func Traces(request *collectortracev1.ExportTraceServiceRequest, options Options) (telemetry.TraceBatch, Report) {
	options = normalizedOptions(options)
	batch := telemetry.TraceBatch{Spans: []telemetry.Span{}, Links: []telemetry.Link{}, Payloads: []telemetry.Payload{}}
	report := Report{Rejections: []Rejection{}}
	if request == nil {
		return batch, report
	}
	receivedAt := options.Now().UTC().Truncate(time.Microsecond)
	for _, resourceSpans := range request.ResourceSpans {
		if resourceSpans == nil {
			continue
		}
		resource, resourceRedacted, resourceError := normalizeAttributes(nil)
		if resourceSpans.Resource != nil {
			resource, resourceRedacted, resourceError = normalizeAttributes(resourceSpans.Resource.Attributes)
		}
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			if scopeSpans == nil {
				continue
			}
			scopeName, scopeVersion := "", ""
			if scopeSpans.Scope != nil {
				scopeName = strings.TrimSpace(scopeSpans.Scope.Name)
				scopeVersion = strings.TrimSpace(scopeSpans.Scope.Version)
			}
			semanticVersion := strings.TrimSpace(scopeSpans.SchemaUrl)
			if semanticVersion == "" {
				semanticVersion = strings.TrimSpace(resourceSpans.SchemaUrl)
			}
			for _, sourceSpan := range scopeSpans.Spans {
				report.Received++
				if resourceError != nil {
					report.Rejections = append(report.Rejections, Rejection{
						TraceID: idHex(sourceSpan.GetTraceId()), SpanID: idHex(sourceSpan.GetSpanId()),
						Reason: "resource attributes are not JSON-compatible",
					})
					continue
				}
				span, links, payloads, err := normalizeSpan(
					sourceSpan, resource, resourceRedacted, scopeName, scopeVersion,
					semanticVersion, receivedAt, options,
				)
				if err != nil {
					report.Rejections = append(report.Rejections, Rejection{
						TraceID: idHex(sourceSpan.GetTraceId()), SpanID: idHex(sourceSpan.GetSpanId()), Reason: err.Error(),
					})
					continue
				}
				batch.Spans = append(batch.Spans, span)
				batch.Links = append(batch.Links, links...)
				batch.Payloads = append(batch.Payloads, payloads...)
				report.Accepted++
			}
		}
	}
	return batch, report
}

func normalizedOptions(options Options) Options {
	if !options.ContentMode.Valid() {
		options.ContentMode = telemetry.ContentModeMetadata
	}
	if options.PayloadLimit <= 0 {
		options.PayloadLimit = defaultPayloadLimit
	}
	if options.PayloadRetention <= 0 {
		options.PayloadRetention = defaultPayloadRetention
	}
	if options.TraceRetention <= 0 {
		options.TraceRetention = defaultTraceRetention
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return options
}

func normalizeSpan(
	source *tracev1.Span,
	resource map[string]any,
	resourceRedacted bool,
	scopeName string,
	scopeVersion string,
	semanticVersion string,
	receivedAt time.Time,
	options Options,
) (telemetry.Span, []telemetry.Link, []telemetry.Payload, error) {
	if source == nil {
		return telemetry.Span{}, nil, nil, fmt.Errorf("span is missing")
	}
	traceID := idHex(source.TraceId)
	spanID := idHex(source.SpanId)
	parentSpanID := idHex(source.ParentSpanId)
	if !telemetry.ValidTraceID(traceID) {
		return telemetry.Span{}, nil, nil, fmt.Errorf("trace ID must be a non-zero 16-byte value")
	}
	if !telemetry.ValidSpanID(spanID) {
		return telemetry.Span{}, nil, nil, fmt.Errorf("span ID must be a non-zero 8-byte value")
	}
	if parentSpanID != "" && !telemetry.ValidSpanID(parentSpanID) {
		return telemetry.Span{}, nil, nil, fmt.Errorf("parent span ID must be empty or a non-zero 8-byte value")
	}
	if strings.TrimSpace(source.Name) == "" {
		return telemetry.Span{}, nil, nil, fmt.Errorf("span name is required")
	}
	startedAt, err := timestamp(source.StartTimeUnixNano, false)
	if err != nil {
		return telemetry.Span{}, nil, nil, fmt.Errorf("invalid start time: %w", err)
	}
	var endedAt *time.Time
	if source.EndTimeUnixNano != 0 {
		value, err := timestamp(source.EndTimeUnixNano, true)
		if err != nil {
			return telemetry.Span{}, nil, nil, fmt.Errorf("invalid end time: %w", err)
		}
		if value.Before(startedAt) {
			return telemetry.Span{}, nil, nil, fmt.Errorf("span end time precedes start time")
		}
		endedAt = &value
	}
	lastAt := startedAt
	if endedAt != nil {
		lastAt = *endedAt
	}
	if lastAt.Before(receivedAt.Add(-options.TraceRetention)) {
		return telemetry.Span{}, nil, nil, fmt.Errorf("span falls outside trace retention")
	}

	attributes, bodies, attributesRedacted, err := splitAttributes(source.Attributes)
	if err != nil {
		return telemetry.Span{}, nil, nil, err
	}
	resourceCopy := cloneMap(resource)
	openInferenceKind := normalizedKind(attributes)
	toolKind := strings.ToLower(firstString(attributes, telemetry.AttributeToolKind))
	mcpMethod := firstString(attributes, telemetry.AttributeMCPMethod)
	countable := countableSpan(attributes, scopeName, openInferenceKind)
	if openInferenceKind == "TOOL" && toolKind == "mcp" && mcpMethod != "tools/call" {
		countable = false
	}
	statusCode, statusMessage := normalizeStatus(source.Status)
	statusBody := bodyValue{}
	if statusMessage != "" {
		cleanStatus, statusRedacted := sanitizeValue(statusMessage)
		statusBody = bodyValue{
			kind: "status.message", contentType: "text/plain", value: cleanStatus,
			redacted: statusRedacted,
		}
		statusMessage = ""
	}
	span := telemetry.Span{
		TraceID: traceID, SpanID: spanID, ParentSpanID: parentSpanID, TraceState: source.TraceState,
		Name: strings.TrimSpace(source.Name), OpenInferenceKind: openInferenceKind, OTelSpanKind: int32(source.Kind),
		StartedAt: startedAt, EndedAt: endedAt, StatusCode: statusCode, StatusMessage: statusMessage,
		AgentID:   firstStringAcross(attributes, resourceCopy, telemetry.AttributeAgentID, "agent.id"),
		SessionID: firstStringAcross(attributes, resourceCopy, telemetry.AttributeSessionID, "session.id"),
		TaskID:    firstStringAcross(attributes, resourceCopy, telemetry.AttributeTaskID, "task.id"),
		Provider:  firstStringAcross(attributes, resourceCopy, "gen_ai.provider.name", "gen_ai.system", "llm.provider"),
		Model:     firstString(attributes, "gen_ai.response.model", "gen_ai.request.model", "llm.model_name"),
		ToolName:  firstString(attributes, "tool.name", "gen_ai.tool.name", "agentshark.tool.name"),
		ToolKind:  toolKind, MCPServer: firstString(attributes, telemetry.AttributeMCPServer),
		PeerAgentID:  firstString(attributes, telemetry.AttributePeerAgentID),
		InputTokens:  nonNegativeInt(attributes, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens", "llm.token_count.prompt"),
		OutputTokens: nonNegativeInt(attributes, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens", "llm.token_count.completion"),
		TotalTokens:  nonNegativeInt(attributes, "gen_ai.usage.total_tokens", "llm.token_count.total"),
		Countable:    countable, Attributes: attributes, Resource: resourceCopy,
		Events: []telemetry.Event{}, InstrumentationScope: scopeName, InstrumentationVersion: scopeVersion,
		SemanticConventionVersion: semanticVersion, ReceivedAt: receivedAt, UpdatedAt: receivedAt,
	}
	if span.TotalTokens == nil {
		span.TotalTokens = tokenTotal(span.InputTokens, span.OutputTokens)
	}
	if explicitVersion := firstStringAcross(attributes, resourceCopy,
		"openinference.semantic_convention.version", "openinference.version", "otel.semconv.version"); explicitVersion != "" {
		span.SemanticConventionVersion = explicitVersion
	}

	payloads := make([]telemetry.Payload, 0, len(bodies)+len(source.Events)+1)
	redacted := resourceRedacted || attributesRedacted
	truncated := false
	contentSeen := len(bodies) > 0 || statusBody.kind != ""
	if statusBody.kind != "" {
		payload, state, captured := makePayload(traceID, spanID, statusBody, receivedAt, options)
		if captured {
			payloads = append(payloads, payload)
		}
		redacted = redacted || state == telemetry.ContentStateRedacted
		truncated = truncated || state == telemetry.ContentStateTruncated
	}
	for _, body := range bodies {
		payload, state, captured := makePayload(traceID, spanID, body, receivedAt, options)
		if captured {
			payloads = append(payloads, payload)
		}
		redacted = redacted || state == telemetry.ContentStateRedacted
		truncated = truncated || state == telemetry.ContentStateTruncated
	}
	for eventIndex, event := range source.Events {
		if event == nil {
			continue
		}
		eventAttributes, eventBodies, eventRedacted, err := splitAttributes(event.Attributes)
		if err != nil {
			return telemetry.Span{}, nil, nil, fmt.Errorf("event %d: %w", eventIndex, err)
		}
		redacted = redacted || eventRedacted
		contentSeen = contentSeen || len(eventBodies) > 0
		for _, body := range eventBodies {
			body.kind = fmt.Sprintf("event.%d.%s", eventIndex, body.kind)
			payload, state, captured := makePayload(traceID, spanID, body, receivedAt, options)
			if captured {
				payloads = append(payloads, payload)
			}
			redacted = redacted || state == telemetry.ContentStateRedacted
			truncated = truncated || state == telemetry.ContentStateTruncated
		}
		eventTime, err := timestamp(event.TimeUnixNano, true)
		if err != nil {
			return telemetry.Span{}, nil, nil, fmt.Errorf("event %d has an invalid time: %w", eventIndex, err)
		}
		span.Events = append(span.Events, telemetry.Event{
			Name: event.Name, Time: eventTime, Attributes: eventAttributes,
			DroppedAttributesCount: event.DroppedAttributesCount,
		})
	}

	links := make([]telemetry.Link, 0, len(source.Links))
	for linkIndex, sourceLink := range source.Links {
		if sourceLink == nil {
			return telemetry.Span{}, nil, nil, fmt.Errorf("link %d is missing", linkIndex)
		}
		linkedTraceID, linkedSpanID := idHex(sourceLink.TraceId), idHex(sourceLink.SpanId)
		if !telemetry.ValidTraceID(linkedTraceID) || !telemetry.ValidSpanID(linkedSpanID) {
			return telemetry.Span{}, nil, nil, fmt.Errorf("link %d has an invalid trace or span ID", linkIndex)
		}
		linkAttributes, linkBodies, linkRedacted, err := splitAttributes(sourceLink.Attributes)
		if err != nil {
			return telemetry.Span{}, nil, nil, fmt.Errorf("link %d: %w", linkIndex, err)
		}
		redacted = redacted || linkRedacted
		contentSeen = contentSeen || len(linkBodies) > 0
		for _, body := range linkBodies {
			body.kind = fmt.Sprintf("link.%d.%s", linkIndex, body.kind)
			payload, state, captured := makePayload(traceID, spanID, body, receivedAt, options)
			if captured {
				payloads = append(payloads, payload)
			}
			redacted = redacted || state == telemetry.ContentStateRedacted
			truncated = truncated || state == telemetry.ContentStateTruncated
		}
		links = append(links, telemetry.Link{
			TraceID: traceID, SpanID: spanID, LinkedTraceID: linkedTraceID,
			LinkedSpanID: linkedSpanID, Attributes: linkAttributes,
		})
	}
	span.ContentState = contentState(options.ContentMode, contentSeen, redacted, truncated, len(payloads) > 0)
	span.ContentState = mergeReportedContentState(
		options.ContentMode,
		span.ContentState,
		strings.ToLower(firstString(attributes, "agentshark.content.state")),
	)
	prepared, err := telemetry.PrepareSpan(span, receivedAt)
	if err != nil {
		return telemetry.Span{}, nil, nil, err
	}
	return prepared, links, uniquePayloads(payloads), nil
}

type bodyValue struct {
	kind        string
	contentType string
	value       any
	redacted    bool
}

func splitAttributes(values []*commonv1.KeyValue) (map[string]any, []bodyValue, bool, error) {
	attributes := make(map[string]any, len(values))
	bodies := []bodyValue{}
	redacted := false
	inputType := attributeString(values, "input.mime_type", "gen_ai.input.mime_type")
	outputType := attributeString(values, "output.mime_type", "gen_ai.output.mime_type")
	for _, keyValue := range values {
		if keyValue == nil || strings.TrimSpace(keyValue.Key) == "" {
			continue
		}
		key := keyValue.Key
		if credentialKey(key) {
			redacted = true
			continue
		}
		value := anyValue(keyValue.Value)
		value, nestedRedacted := sanitizeValue(value)
		if _, err := json.Marshal(value); err != nil {
			return nil, nil, false, fmt.Errorf("attribute %q is not JSON-compatible", key)
		}
		redacted = redacted || nestedRedacted
		if kind, content := bodyKind(key); content {
			contentType := "application/json"
			if strings.HasPrefix(kind, "input") && inputType != "" {
				contentType = inputType
			}
			if strings.HasPrefix(kind, "output") && outputType != "" {
				contentType = outputType
			}
			bodies = append(bodies, bodyValue{kind: kind, contentType: contentType, value: value, redacted: nestedRedacted})
			continue
		}
		attributes[key] = value
	}
	return attributes, bodies, redacted, nil
}

func normalizeAttributes(values []*commonv1.KeyValue) (map[string]any, bool, error) {
	attributes, bodies, redacted, err := splitAttributes(values)
	return attributes, redacted || len(bodies) > 0, err
}

func anyValue(value *commonv1.AnyValue) any {
	if value == nil {
		return nil
	}
	switch typed := value.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return typed.StringValue
	case *commonv1.AnyValue_BoolValue:
		return typed.BoolValue
	case *commonv1.AnyValue_IntValue:
		return typed.IntValue
	case *commonv1.AnyValue_DoubleValue:
		return typed.DoubleValue
	case *commonv1.AnyValue_BytesValue:
		return append([]byte(nil), typed.BytesValue...)
	case *commonv1.AnyValue_ArrayValue:
		values := make([]any, 0, len(typed.ArrayValue.GetValues()))
		for _, item := range typed.ArrayValue.GetValues() {
			values = append(values, anyValue(item))
		}
		return values
	case *commonv1.AnyValue_KvlistValue:
		values := make(map[string]any, len(typed.KvlistValue.GetValues()))
		for _, item := range typed.KvlistValue.GetValues() {
			if item != nil {
				values[item.Key] = anyValue(item.Value)
			}
		}
		return values
	default:
		return nil
	}
}

func sanitizeValue(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		if credentialValue(typed) {
			return "[REDACTED]", true
		}
		return typed, false
	case []byte:
		if credentialValue(string(typed)) {
			return []byte("[REDACTED]"), true
		}
		return append([]byte(nil), typed...), false
	case map[string]any:
		result := make(map[string]any, len(typed))
		redacted := false
		for key, item := range typed {
			if credentialKey(key) {
				redacted = true
				continue
			}
			clean, childRedacted := sanitizeValue(item)
			result[key] = clean
			redacted = redacted || childRedacted
		}
		return result, redacted
	case []any:
		result := make([]any, 0, len(typed))
		redacted := false
		for _, item := range typed {
			clean, childRedacted := sanitizeValue(item)
			result = append(result, clean)
			redacted = redacted || childRedacted
		}
		return result, redacted
	default:
		return value, false
	}
}

func credentialValue(value string) bool {
	return credentialValuePattern.MatchString(value)
}

func credentialKey(value string) bool {
	key := canonicalKey(value)
	for _, fragment := range []string{
		"authorization", "proxy.authorization", "cookie", "set.cookie", "api.key", "apikey",
		"access.token", "refresh.token", "client.secret", "secret.access.key", "password", "passwd",
		"credential", "credentials", "secret", "session.key",
	} {
		if strings.Contains("."+key+".", "."+fragment+".") {
			return true
		}
	}
	return false
}

func bodyKind(value string) (string, bool) {
	key := canonicalKey(value)
	kinds := map[string]string{
		"input.value": "input", "llm.input.messages": "input", "llm.input_messages": "input",
		"llm.prompts": "input.prompts", "llm.prompt.template.template": "input.prompt_template",
		"llm.prompt.template.variables": "input.prompt_variables", "metadata": "metadata",
		"gen.ai.prompt": "input", "gen.ai.input.messages": "input", "gen.ai.system.instructions": "input.system",
		"output.value": "output", "llm.output.messages": "output", "llm.output_messages": "output",
		"llm.choices": "output.choices", "llm.tools": "tool.definitions",
		"gen.ai.completion": "output", "gen.ai.output.messages": "output",
		"llm.invocation.parameters": "llm.invocation", "llm.invocation_parameters": "llm.invocation",
		"tool.arguments": "tool.arguments", "tool.input": "tool.arguments", "tool.call.arguments": "tool.arguments",
		"tool.parameters": "tool.arguments", "tool.call.function.arguments": "tool.arguments",
		"gen.ai.tool.call.arguments": "tool.arguments", "tool.result": "tool.result", "tool.output": "tool.result",
		"gen.ai.tool.call.result": "tool.result", "agentshark.task.goal": "task.goal",
		"agentshark.payload.input": "input", "agentshark.payload.output": "output",
		"retrieval.documents": "retrieval.documents", "embedding.embeddings": "embedding.embeddings",
		"reranker.input.documents":  "reranker.input_documents",
		"reranker.output.documents": "reranker.output_documents",
		"exception.message":         "exception.message", "exception.stacktrace": "exception.stacktrace",
		"exception.stack.trace": "exception.stacktrace",
	}
	if kind, found := kinds[key]; found {
		return kind, true
	}
	prefixes := []struct{ prefix, kind string }{
		{"llm.input.messages.", "input.messages."}, {"llm.output.messages.", "output.messages."},
		{"llm.prompts.", "input.prompts."}, {"llm.choices.", "output.choices."},
		{"llm.tools.", "tool.definitions."}, {"embedding.embeddings.", "embedding.embeddings."},
		{"retrieval.documents.", "retrieval.documents."},
		{"reranker.input.documents.", "reranker.input_documents."},
		{"reranker.output.documents.", "reranker.output_documents."},
		{"gen.ai.input.messages.", "input.messages."}, {"gen.ai.output.messages.", "output.messages."},
		{"gen.ai.retrieval.", "retrieval."},
	}
	for _, candidate := range prefixes {
		if strings.HasPrefix(key, candidate.prefix) {
			return candidate.kind + strings.TrimPrefix(key, candidate.prefix), true
		}
	}
	return "", false
}

func canonicalKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", ".", "_", ".", "/", ".").Replace(value)
	for strings.Contains(value, "..") {
		value = strings.ReplaceAll(value, "..", ".")
	}
	return value
}

func makePayload(
	traceID string,
	spanID string,
	body bodyValue,
	receivedAt time.Time,
	options Options,
) (telemetry.Payload, string, bool) {
	if options.ContentMode != telemetry.ContentModeFull {
		return telemetry.Payload{}, telemetry.ContentStateRedacted, false
	}
	document, err := json.Marshal(body.value)
	if err != nil {
		return telemetry.Payload{}, telemetry.ContentStateRedacted, false
	}
	expiresAt := receivedAt.Add(options.PayloadRetention)
	payload := telemetry.Payload{
		TraceID: traceID, SpanID: spanID, Kind: body.kind, ContentType: body.contentType, Encoding: "identity",
		RedactionState: telemetry.ContentStateCaptured, SizeBytes: int64(len(document)), ExpiresAt: &expiresAt, CreatedAt: receivedAt,
	}
	state := telemetry.ContentStateCaptured
	if int64(len(document)) > options.PayloadLimit {
		payload.PayloadBytes = append([]byte(nil), document[:options.PayloadLimit]...)
		payload.RedactionState = telemetry.ContentStateTruncated
		state = telemetry.ContentStateTruncated
	} else {
		payload.PayloadJSON = append(json.RawMessage(nil), document...)
	}
	if body.redacted {
		payload.RedactionState = telemetry.ContentStateRedacted
		state = telemetry.ContentStateRedacted
	}
	return payload, state, true
}

func contentState(mode telemetry.ContentMode, contentSeen, redacted, truncated, captured bool) string {
	if mode == telemetry.ContentModeNone || !contentSeen {
		if redacted && mode != telemetry.ContentModeNone {
			return telemetry.ContentStateRedacted
		}
		return telemetry.ContentStateNotCollected
	}
	if truncated {
		return telemetry.ContentStateTruncated
	}
	if redacted || mode == telemetry.ContentModeMetadata {
		return telemetry.ContentStateRedacted
	}
	if captured {
		return telemetry.ContentStateCaptured
	}
	return telemetry.ContentStateNotCollected
}

func mergeReportedContentState(mode telemetry.ContentMode, computed, reported string) string {
	if mode == telemetry.ContentModeNone {
		return telemetry.ContentStateNotCollected
	}
	if mode == telemetry.ContentModeMetadata {
		if computed == telemetry.ContentStateRedacted || reported == telemetry.ContentStateRedacted ||
			reported == telemetry.ContentStateTruncated {
			return telemetry.ContentStateRedacted
		}
		return computed
	}
	rank := map[string]int{
		telemetry.ContentStateNotCollected: 0,
		telemetry.ContentStateRedacted:     2,
		telemetry.ContentStateTruncated:    3,
	}
	if rank[reported] > rank[computed] {
		return reported
	}
	return computed
}

func uniquePayloads(payloads []telemetry.Payload) []telemetry.Payload {
	unique := make([]telemetry.Payload, 0, len(payloads))
	seen := make(map[string]int, len(payloads))
	for _, payload := range payloads {
		baseKind := payload.Kind
		seen[baseKind]++
		if seen[baseKind] > 1 {
			payload.Kind = fmt.Sprintf("%s.%d", baseKind, seen[baseKind])
		}
		unique = append(unique, payload)
	}
	return unique
}

func normalizedKind(attributes map[string]any) string {
	if value := strings.ToUpper(firstString(attributes, "openinference.span.kind")); value != "" {
		return value
	}
	if strings.EqualFold(firstString(attributes, telemetry.AttributeSpanKind), "task") {
		return "AGENT"
	}
	switch firstString(attributes, "gen_ai.operation.name") {
	case "chat", "text_completion", "generate_content":
		return "LLM"
	case "embeddings":
		return "EMBEDDING"
	case "execute_tool":
		return "TOOL"
	case "retrieval":
		return "RETRIEVER"
	case "invoke_agent":
		return "AGENT"
	default:
		return ""
	}
}

func countableSpan(attributes map[string]any, scopeName, kind string) bool {
	if explicit, exists := boolAttribute(attributes, telemetry.AttributeCountable); exists {
		return explicit
	}
	switch strings.ToLower(scopeName) {
	case "openinference.instrumentation.langchain", "opentelemetry.instrumentation.langchain":
		return kind == "LLM" || kind == "TOOL" || kind == "RETRIEVER"
	default:
		return false
	}
}

func normalizeStatus(status *tracev1.Status) (string, string) {
	if status == nil {
		return telemetry.StatusUnset, ""
	}
	switch status.Code {
	case tracev1.Status_STATUS_CODE_OK:
		return telemetry.StatusOK, status.Message
	case tracev1.Status_STATUS_CODE_ERROR:
		return telemetry.StatusError, status.Message
	default:
		return telemetry.StatusUnset, status.Message
	}
}

func timestamp(value uint64, allowZero bool) (time.Time, error) {
	if value == 0 && !allowZero {
		return time.Time{}, fmt.Errorf("timestamp is required")
	}
	if value > math.MaxInt64 {
		return time.Time{}, fmt.Errorf("timestamp exceeds supported range")
	}
	return time.Unix(0, int64(value)).UTC().Truncate(time.Microsecond), nil
}

func idHex(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return hex.EncodeToString(value)
}

func firstString(attributes map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attributes[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstStringAcross(spanAttributes, resourceAttributes map[string]any, keys ...string) string {
	if value := firstString(spanAttributes, keys...); value != "" {
		return value
	}
	return firstString(resourceAttributes, keys...)
}

func attributeString(values []*commonv1.KeyValue, keys ...string) string {
	for _, item := range values {
		if item == nil {
			continue
		}
		for _, key := range keys {
			if item.Key == key {
				if value, ok := anyValue(item.Value).(string); ok {
					return value
				}
			}
		}
	}
	return ""
}

func boolAttribute(attributes map[string]any, key string) (bool, bool) {
	value, ok := attributes[key].(bool)
	return value, ok
}

func nonNegativeInt(attributes map[string]any, keys ...string) *int64 {
	for _, key := range keys {
		if value, ok := attributes[key].(int64); ok && value >= 0 {
			result := value
			return &result
		}
	}
	return nil
}

func tokenTotal(input, output *int64) *int64 {
	if input == nil && output == nil {
		return nil
	}
	var total int64
	if input != nil {
		total = *input
	}
	if output != nil {
		if *output > math.MaxInt64-total {
			return nil
		}
		total += *output
	}
	return &total
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
