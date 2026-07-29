// Command e2e-trace sends one deterministic OTLP Trace fixture to a Collector.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	defaultEndpoint = "http://127.0.0.1:19418/v1/traces"
	traceTokenKey   = "AGENTSHARK_E2E_TRACE_TOKEN"
)

func main() {
	token := strings.TrimSpace(os.Getenv(traceTokenKey))
	if token == "" {
		log.Fatalf("%s is required", traceTokenKey)
	}
	endpoint := strings.TrimSpace(os.Getenv("AGENTSHARK_E2E_TRACE_ENDPOINT"))
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sendTrace(ctx, http.DefaultClient, endpoint, token, time.Now().UTC()); err != nil {
		log.Fatal(err)
	}
}

func sendTrace(ctx context.Context, client *http.Client, endpoint, token string, startedAt time.Time) error {
	document, err := proto.Marshal(exportRequest(startedAt))
	if err != nil {
		return fmt.Errorf("marshal OTLP fixture: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(document))
	if err != nil {
		return fmt.Errorf("create Collector request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/x-protobuf")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send OTLP fixture: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Collector response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Collector returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var exportResponse collectortracev1.ExportTraceServiceResponse
	if err := proto.Unmarshal(body, &exportResponse); err != nil {
		return fmt.Errorf("decode Collector response: %w", err)
	}
	if rejected := exportResponse.GetPartialSuccess().GetRejectedSpans(); rejected != 0 {
		return fmt.Errorf("Collector rejected %d fixture spans", rejected)
	}
	return nil
}

func exportRequest(startedAt time.Time) *collectortracev1.ExportTraceServiceRequest {
	startedAt = startedAt.UTC().Truncate(time.Microsecond)
	endedAt := startedAt.Add(25 * time.Millisecond)
	return &collectortracev1.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
		Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{
			{Key: "service.name", Value: stringValue("agentshark-release-e2e")},
			{Key: "agentshark.agent.id", Value: stringValue("release-agent")},
			{Key: "agentshark.session.id", Value: stringValue("release-session")},
		}},
		ScopeSpans: []*tracev1.ScopeSpans{{
			Scope: &commonv1.InstrumentationScope{Name: "agentshark.release-e2e", Version: "1"},
			Spans: []*tracev1.Span{{
				TraceId:           bytes.Repeat([]byte{0x11}, 16),
				SpanId:            bytes.Repeat([]byte{0x22}, 8),
				Name:              "agentshark.release.task",
				Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
				StartTimeUnixNano: uint64(startedAt.UnixNano()),
				EndTimeUnixNano:   uint64(endedAt.UnixNano()),
				Attributes: []*commonv1.KeyValue{
					{Key: "agentshark.task.id", Value: stringValue("release-task")},
					{Key: "agentshark.task.root", Value: boolValue(true)},
					{Key: "agentshark.span.kind", Value: stringValue("task")},
				},
				Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
			}},
		}},
	}}}
}

func stringValue(value string) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}
}

func boolValue(value bool) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: value}}
}
