package normalize

import (
	"bytes"
	"encoding/base64"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

var (
	testTraceID       = bytes.Repeat([]byte{0x11}, 16)
	testSpanID        = bytes.Repeat([]byte{0x22}, 8)
	testLinkedTraceID = bytes.Repeat([]byte{0x33}, 16)
	testLinkedSpanID  = bytes.Repeat([]byte{0x44}, 8)
	testNow           = time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
)

func TestTracesMapsSemanticFieldsAndPreservesUnknownAttributes(t *testing.T) {
	span := validOTLPSpan()
	span.Attributes = []*commonv1.KeyValue{
		stringKV("openinference.span.kind", "LLM"),
		stringKV("gen_ai.provider.name", "openai"),
		stringKV("gen_ai.response.model", "gpt-test"),
		intKV("gen_ai.usage.input_tokens", 12), intKV("gen_ai.usage.output_tokens", 7),
		stringKV(telemetry.AttributeSessionID, "session-1"), stringKV(telemetry.AttributeTaskID, "task-1"),
		stringKV("custom.future.attribute", "preserved"),
		stringKV("input.value", "sensitive prompt"),
		stringKV("http.request.header.authorization", "Bearer secret"),
		{Key: "custom.object", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_KvlistValue{KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{
			stringKV("safe", "yes"), stringKV("api_key", "remove-me"),
		}}}}},
	}
	span.Events = []*tracev1.Span_Event{{
		Name: "completion", TimeUnixNano: uint64(testNow.Add(1500 * time.Millisecond).UnixNano()),
		Attributes: []*commonv1.KeyValue{stringKV("output.value", "sensitive completion"), stringKV("event.future", "preserved")},
	}}
	span.Links = []*tracev1.Span_Link{{
		TraceId: testLinkedTraceID, SpanId: testLinkedSpanID,
		Attributes: []*commonv1.KeyValue{stringKV("messaging.operation", "publish")},
	}}
	request := requestFor("openinference.instrumentation.langchain", span)
	request.ResourceSpans[0].Resource = &resourcev1.Resource{Attributes: []*commonv1.KeyValue{
		stringKV(telemetry.AttributeAgentID, "agent-1"), stringKV("service.name", "research-worker"),
	}}

	batch, report := Traces(request, Options{Now: func() time.Time { return testNow }})
	if report.Received != 1 || report.Accepted != 1 || len(report.Rejections) != 0 {
		t.Fatalf("report = %#v", report)
	}
	if len(batch.Spans) != 1 || len(batch.Links) != 1 || len(batch.Payloads) != 0 {
		t.Fatalf("batch sizes = spans %d links %d payloads %d", len(batch.Spans), len(batch.Links), len(batch.Payloads))
	}
	normalized := batch.Spans[0]
	if normalized.OpenInferenceKind != "LLM" || !normalized.Countable || normalized.AgentID != "agent-1" ||
		normalized.SessionID != "session-1" || normalized.TaskID != "task-1" || normalized.Provider != "openai" ||
		normalized.Model != "gpt-test" || normalized.InputTokens == nil || *normalized.InputTokens != 12 ||
		normalized.OutputTokens == nil || *normalized.OutputTokens != 7 || normalized.TotalTokens == nil || *normalized.TotalTokens != 19 {
		t.Fatalf("normalized fields = %#v", normalized)
	}
	if normalized.Attributes["custom.future.attribute"] != "preserved" || normalized.Resource["service.name"] != "research-worker" {
		t.Fatalf("unknown attributes were not retained: %#v / %#v", normalized.Attributes, normalized.Resource)
	}
	if _, exists := normalized.Attributes["input.value"]; exists {
		t.Fatal("input content remained in attributes_json")
	}
	if _, exists := normalized.Attributes["http.request.header.authorization"]; exists {
		t.Fatal("authorization remained in attributes_json")
	}
	customObject := normalized.Attributes["custom.object"].(map[string]any)
	if customObject["safe"] != "yes" {
		t.Fatalf("nested safe value missing: %#v", customObject)
	}
	if _, exists := customObject["api_key"]; exists {
		t.Fatal("nested credential remained in attributes_json")
	}
	if normalized.ContentState != telemetry.ContentStateRedacted || len(normalized.Events) != 1 ||
		normalized.Events[0].Attributes["event.future"] != "preserved" {
		t.Fatalf("event/content normalization = %#v", normalized)
	}
	if _, exists := normalized.Events[0].Attributes["output.value"]; exists {
		t.Fatal("event body remained in events_json")
	}
	if batch.Links[0].LinkedTraceID != strings.Repeat("33", 16) || batch.Links[0].Attributes["messaging.operation"] != "publish" {
		t.Fatalf("link = %#v", batch.Links[0])
	}
}

func TestTracesContentModes(t *testing.T) {
	tests := []struct {
		name             string
		mode             telemetry.ContentMode
		limit            int64
		wantState        string
		wantPayloads     int
		wantPayloadState string
	}{
		{name: "none", mode: telemetry.ContentModeNone, wantState: telemetry.ContentStateNotCollected},
		{name: "metadata", mode: telemetry.ContentModeMetadata, wantState: telemetry.ContentStateRedacted},
		{name: "full", mode: telemetry.ContentModeFull, limit: 1024, wantState: telemetry.ContentStateCaptured, wantPayloads: 2, wantPayloadState: telemetry.ContentStateCaptured},
		{name: "full truncated", mode: telemetry.ContentModeFull, limit: 5, wantState: telemetry.ContentStateTruncated, wantPayloads: 2, wantPayloadState: telemetry.ContentStateTruncated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			span := validOTLPSpan()
			span.Attributes = []*commonv1.KeyValue{
				stringKV("input.value", "prompt"), stringKV("output.value", "completion"),
			}
			batch, report := Traces(requestFor("openinference.instrumentation.langchain", span), Options{
				ContentMode: test.mode, PayloadLimit: test.limit, PayloadRetention: time.Hour,
				Now: func() time.Time { return testNow },
			})
			if report.Accepted != 1 || len(batch.Spans) != 1 || batch.Spans[0].ContentState != test.wantState || len(batch.Payloads) != test.wantPayloads {
				t.Fatalf("batch/report = %#v / %#v", batch, report)
			}
			for _, payload := range batch.Payloads {
				if payload.RedactionState != test.wantPayloadState {
					t.Fatalf("payload state = %#v", payload)
				}
				document := append([]byte(nil), payload.PayloadJSON...)
				document = append(document, payload.PayloadBytes...)
				if payload.ExpiresAt == nil || !payload.ExpiresAt.Equal(testNow.Add(time.Hour)) {
					t.Fatalf("payload expiry = %v", payload.ExpiresAt)
				}
			}
		})
	}
}

func TestTracesRecognizesSDKContentPrefixesAndReportedState(t *testing.T) {
	span := validOTLPSpan()
	span.Attributes = []*commonv1.KeyValue{
		stringKV("llm.input_messages.0.message.content", "private prompt"),
		stringKV("retrieval.documents.0.document.content", "private document"),
		stringKV("agentshark.content.state", telemetry.ContentStateRedacted),
	}
	metadata, report := Traces(requestFor("agentshark.sdk", span), Options{
		ContentMode: telemetry.ContentModeMetadata, Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || metadata.Spans[0].ContentState != telemetry.ContentStateRedacted {
		t.Fatalf("metadata batch/report = %#v / %#v", metadata, report)
	}
	for key := range metadata.Spans[0].Attributes {
		if strings.Contains(key, "input_messages") || strings.Contains(key, "retrieval.documents") {
			t.Fatalf("SDK content prefix remained in attributes: %s", key)
		}
	}

	full, report := Traces(requestFor("agentshark.sdk", span), Options{
		ContentMode: telemetry.ContentModeFull, PayloadLimit: 1024, PayloadRetention: time.Hour,
		Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || len(full.Payloads) != 2 ||
		full.Spans[0].ContentState != telemetry.ContentStateRedacted {
		t.Fatalf("full batch/report = %#v / %#v", full, report)
	}
}

func TestBodyKindRecognizesSDKContentPrefixes(t *testing.T) {
	keys := []string{
		"llm.input_messages.0.message.content",
		"llm.output_messages.0.message.content",
		"llm.prompts.0",
		"llm.choices.0.message.content",
		"llm.tools.0.function.parameters",
		"embedding.embeddings.0.vector",
		"retrieval.documents.0.document.content",
		"reranker.input_documents.0.document.content",
		"reranker.output_documents.0.document.content",
		"gen_ai.input.messages.0.content",
		"gen_ai.output.messages.0.content",
		"gen_ai.retrieval.documents.0.content",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			if _, content := bodyKind(key); !content {
				t.Fatalf("SDK content key %q was not recognized", key)
			}
		})
	}
}

func TestTracesPreservesReportedContentStateAfterSDKStripsBodies(t *testing.T) {
	span := validOTLPSpan()
	span.Attributes = []*commonv1.KeyValue{
		stringKV("agentshark.content.state", telemetry.ContentStateRedacted),
		intKV("agentshark.task.goal.length", 17),
	}
	batch, report := Traces(requestFor("agentshark.sdk", span), Options{
		ContentMode: telemetry.ContentModeMetadata, Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || len(batch.Spans) != 1 ||
		batch.Spans[0].ContentState != telemetry.ContentStateRedacted || len(batch.Payloads) != 0 {
		t.Fatalf("batch/report = %#v / %#v", batch, report)
	}

	none, report := Traces(requestFor("agentshark.sdk", span), Options{
		ContentMode: telemetry.ContentModeNone, Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || none.Spans[0].ContentState != telemetry.ContentStateNotCollected {
		t.Fatalf("none batch/report = %#v / %#v", none, report)
	}
}

func TestTracesDoesNotTrustReportedCapturedStateWithoutContent(t *testing.T) {
	span := validOTLPSpan()
	span.Attributes = []*commonv1.KeyValue{
		stringKV("agentshark.content.state", telemetry.ContentStateCaptured),
	}
	for _, mode := range []telemetry.ContentMode{telemetry.ContentModeMetadata, telemetry.ContentModeFull} {
		batch, report := Traces(requestFor("untrusted-client", span), Options{
			ContentMode: mode, Now: func() time.Time { return testNow },
		})
		if report.Accepted != 1 || batch.Spans[0].ContentState != telemetry.ContentStateNotCollected ||
			len(batch.Payloads) != 0 {
			t.Fatalf("mode %s batch/report = %#v / %#v", mode, batch, report)
		}
	}
}

func TestTracesRejectsNonFiniteAttributesIndependently(t *testing.T) {
	values := map[string]float64{
		"NaN": math.NaN(), "+Inf": math.Inf(1), "-Inf": math.Inf(-1),
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			invalid := validOTLPSpan()
			invalid.Attributes = []*commonv1.KeyValue{{
				Key:   "custom.nonfinite",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: value}},
			}}
			valid := validOTLPSpan()
			valid.SpanId = bytes.Repeat([]byte{0x55}, 8)
			batch, report := Traces(requestFor("scope", invalid, valid), Options{
				Now: func() time.Time { return testNow },
			})
			if report.Received != 2 || report.Accepted != 1 || len(report.Rejections) != 1 ||
				!strings.Contains(report.Rejections[0].Reason, "JSON-compatible") || len(batch.Spans) != 1 {
				t.Fatalf("batch/report = %#v / %#v", batch, report)
			}
		})
	}
}

func TestTracesRejectsExpiredReplayWithoutRejectingCurrentSibling(t *testing.T) {
	expired := validOTLPSpan()
	expired.StartTimeUnixNano = uint64(testNow.Add(-2 * time.Hour).UnixNano())
	expired.EndTimeUnixNano = uint64(testNow.Add(-2*time.Hour + time.Second).UnixNano())
	current := validOTLPSpan()
	current.SpanId = bytes.Repeat([]byte{0x66}, 8)

	batch, report := Traces(requestFor("scope", expired, current), Options{
		ContentMode:    telemetry.ContentModeMetadata,
		TraceRetention: time.Hour,
		Now:            func() time.Time { return testNow },
	})
	if report.Received != 2 || report.Accepted != 1 || report.Rejected() != 1 ||
		len(batch.Spans) != 1 || batch.Spans[0].SpanID != strings.Repeat("66", 8) ||
		!strings.Contains(report.Rejections[0].Reason, "retention") {
		t.Fatalf("batch/report = %#v / %#v", batch, report)
	}
}

func TestTracesKeepsStatusAndExceptionBodiesOutOfMetadata(t *testing.T) {
	span := validOTLPSpan()
	span.Status = &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR, Message: "private prompt failed"}
	span.Events = []*tracev1.Span_Event{{
		Name: "exception", TimeUnixNano: uint64(testNow.Add(time.Second).UnixNano()),
		Attributes: []*commonv1.KeyValue{
			stringKV("exception.type", "ValueError"),
			stringKV("exception.message", "private completion"),
			stringKV("exception.stacktrace", "authorization: Bearer secret"),
		},
	}}

	metadata, report := Traces(requestFor("agentshark.sdk", span), Options{
		ContentMode: telemetry.ContentModeMetadata, Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || metadata.Spans[0].StatusMessage != "" || len(metadata.Payloads) != 0 {
		t.Fatalf("metadata batch/report = %#v / %#v", metadata, report)
	}
	if metadata.Spans[0].Events[0].Attributes["exception.type"] != "ValueError" {
		t.Fatalf("exception type missing: %#v", metadata.Spans[0].Events)
	}
	for _, key := range []string{"exception.message", "exception.stacktrace"} {
		if _, exists := metadata.Spans[0].Events[0].Attributes[key]; exists {
			t.Fatalf("%s remained in events metadata", key)
		}
	}

	full, report := Traces(requestFor("agentshark.sdk", span), Options{
		ContentMode: telemetry.ContentModeFull, PayloadLimit: 1024, PayloadRetention: time.Hour,
		Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || len(full.Payloads) != 3 {
		t.Fatalf("full batch/report = %#v / %#v", full, report)
	}
	for _, payload := range full.Payloads {
		document := string(payload.PayloadJSON) + string(payload.PayloadBytes)
		if strings.Contains(strings.ToLower(document), "bearer secret") {
			t.Fatalf("credential value reached payload %q", payload.Kind)
		}
	}
}

func TestTracesRedactsCredentialsFromStatusAndStringifiedJSON(t *testing.T) {
	binaryCredential := []byte("authorization: Bearer binary-secret")
	span := validOTLPSpan()
	span.Status = &tracev1.Status{
		Code:    tracev1.Status_STATUS_CODE_ERROR,
		Message: `{"api_key":"status-secret"}`,
	}
	span.Attributes = []*commonv1.KeyValue{
		stringKV("input.value", `{"credentials":"attribute-secret"}`),
		{Key: "output.value", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BytesValue{BytesValue: binaryCredential}}},
		stringKV("custom.session_key", "never-store"),
	}
	batch, report := Traces(requestFor("scope", span), Options{
		ContentMode: telemetry.ContentModeFull, PayloadLimit: 1024, PayloadRetention: time.Hour,
		Now: func() time.Time { return testNow },
	})
	if report.Accepted != 1 || batch.Spans[0].ContentState != telemetry.ContentStateRedacted ||
		len(batch.Payloads) != 3 {
		t.Fatalf("batch/report = %#v / %#v", batch, report)
	}
	if _, exists := batch.Spans[0].Attributes["custom.session_key"]; exists {
		t.Fatal("session key remained in attributes")
	}
	encodedBinaryCredential := base64.StdEncoding.EncodeToString(binaryCredential)
	for _, payload := range batch.Payloads {
		document := string(payload.PayloadJSON) + string(payload.PayloadBytes)
		if strings.Contains(document, "status-secret") || strings.Contains(document, "attribute-secret") ||
			strings.Contains(document, encodedBinaryCredential) {
			t.Fatalf("credential remained in %s", payload.Kind)
		}
	}
}

func TestTracesRejectsInvalidSpansIndependently(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tracev1.Span)
		part   string
	}{
		{name: "trace ID", mutate: func(span *tracev1.Span) { span.TraceId = []byte{1} }, part: "trace ID"},
		{name: "zero trace ID", mutate: func(span *tracev1.Span) { span.TraceId = make([]byte, 16) }, part: "trace ID"},
		{name: "span ID", mutate: func(span *tracev1.Span) { span.SpanId = []byte{1} }, part: "span ID"},
		{name: "parent span ID", mutate: func(span *tracev1.Span) { span.ParentSpanId = []byte{1} }, part: "parent span ID"},
		{name: "missing name", mutate: func(span *tracev1.Span) { span.Name = " " }, part: "name"},
		{name: "missing start", mutate: func(span *tracev1.Span) { span.StartTimeUnixNano = 0 }, part: "start time"},
		{name: "end before start", mutate: func(span *tracev1.Span) { span.EndTimeUnixNano = span.StartTimeUnixNano - 1 }, part: "precedes"},
		{name: "invalid link", mutate: func(span *tracev1.Span) {
			span.Links = []*tracev1.Span_Link{{TraceId: []byte{1}, SpanId: testLinkedSpanID}}
		}, part: "link"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := validOTLPSpan()
			test.mutate(invalid)
			batch, report := Traces(requestFor("scope", invalid, validOTLPSpan()), Options{Now: func() time.Time { return testNow }})
			if report.Received != 2 || report.Accepted != 1 || len(report.Rejections) != 1 ||
				!strings.Contains(report.Rejections[0].Reason, test.part) || len(batch.Spans) != 1 {
				t.Fatalf("batch/report = %#v / %#v", batch, report)
			}
		})
	}
}

func TestTracesUsesExplicitMCPAndA2AClassification(t *testing.T) {
	tests := []struct {
		name          string
		attributes    []*commonv1.KeyValue
		scope         string
		wantKind      string
		wantCountable bool
		wantToolKind  string
		wantPeer      string
	}{
		{name: "canonical LangChain LLM", scope: "openinference.instrumentation.langchain", attributes: []*commonv1.KeyValue{stringKV("openinference.span.kind", "LLM")}, wantKind: "LLM", wantCountable: true},
		{name: "lower-level LLM", scope: "opentelemetry.instrumentation.openai", attributes: []*commonv1.KeyValue{stringKV("openinference.span.kind", "LLM")}, wantKind: "LLM"},
		{name: "MCP tool call", scope: "agentshark.sdk", attributes: []*commonv1.KeyValue{
			stringKV("openinference.span.kind", "TOOL"), boolKV(telemetry.AttributeCountable, true),
			stringKV(telemetry.AttributeToolKind, "mcp"), stringKV(telemetry.AttributeMCPMethod, "tools/call"),
		}, wantKind: "TOOL", wantCountable: true, wantToolKind: "mcp"},
		{name: "MCP protocol event", scope: "agentshark.sdk", attributes: []*commonv1.KeyValue{
			stringKV("openinference.span.kind", "TOOL"), boolKV(telemetry.AttributeCountable, true),
			stringKV(telemetry.AttributeToolKind, "mcp"), stringKV(telemetry.AttributeMCPMethod, "tools/list"),
		}, wantKind: "TOOL", wantToolKind: "mcp"},
		{name: "A2A", scope: "agentshark.sdk", attributes: []*commonv1.KeyValue{
			stringKV("gen_ai.operation.name", "invoke_agent"), boolKV(telemetry.AttributeCountable, true),
			stringKV(telemetry.AttributePeerAgentID, "planner"),
		}, wantKind: "AGENT", wantCountable: true, wantPeer: "planner"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			span := validOTLPSpan()
			span.Attributes = test.attributes
			batch, report := Traces(requestFor(test.scope, span), Options{Now: func() time.Time { return testNow }})
			if report.Accepted != 1 {
				t.Fatalf("report = %#v", report)
			}
			got := batch.Spans[0]
			if got.OpenInferenceKind != test.wantKind || got.Countable != test.wantCountable ||
				got.ToolKind != test.wantToolKind || got.PeerAgentID != test.wantPeer {
				t.Fatalf("span = %#v", got)
			}
		})
	}
}

func validOTLPSpan() *tracev1.Span {
	return &tracev1.Span{
		TraceId: append([]byte(nil), testTraceID...), SpanId: append([]byte(nil), testSpanID...), Name: "operation",
		StartTimeUnixNano: uint64(testNow.UnixNano()), EndTimeUnixNano: uint64(testNow.Add(2 * time.Second).UnixNano()),
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
	}
}

func requestFor(scope string, spans ...*tracev1.Span) *collectortracev1.ExportTraceServiceRequest {
	return &collectortracev1.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
		ScopeSpans: []*tracev1.ScopeSpans{{Scope: &commonv1.InstrumentationScope{Name: scope, Version: "1.2.3"}, SchemaUrl: "semconv-1", Spans: spans}},
	}}}
}

func stringKV(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}}
}

func intKV(key string, value int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: value}}}
}

func boolKV(key string, value bool) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: value}}}
}
