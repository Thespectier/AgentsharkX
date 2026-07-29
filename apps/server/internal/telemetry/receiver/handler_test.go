package receiver

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const testIngestToken = "collector-ingest-token-for-tests"

type recordingWriter struct {
	mu      sync.Mutex
	batches []telemetry.TraceBatch
	result  telemetry.WriteResult
	err     error
	write   func(context.Context, telemetry.TraceBatch) (telemetry.WriteResult, error)
}

type delayedReader struct {
	delay time.Duration
	data  []byte
}

func (reader *delayedReader) Read(destination []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(reader.delay)
	written := copy(destination, reader.data)
	reader.data = reader.data[written:]
	return written, nil
}

func (writer *recordingWriter) WriteBatch(ctx context.Context, batch telemetry.TraceBatch) (telemetry.WriteResult, error) {
	writer.mu.Lock()
	writer.batches = append(writer.batches, batch)
	writer.mu.Unlock()
	if writer.write != nil {
		return writer.write(ctx, batch)
	}
	return writer.result, writer.err
}

func (writer *recordingWriter) batchCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return len(writer.batches)
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Options{IngestToken: testIngestToken}); !errors.Is(err, errWriterRequired) {
		t.Fatalf("nil writer error = %v", err)
	}
	if _, err := New(&recordingWriter{}, Options{IngestToken: "too-short"}); !errors.Is(err, errTokenRequired) {
		t.Fatalf("short token error = %v", err)
	}
	if _, err := New(&recordingWriter{}, Options{
		IngestToken: testIngestToken, MaxCompressedBytes: 2, MaxDecompressedBytes: 1,
	}); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := New(&recordingWriter{}, Options{
		IngestToken: testIngestToken, MaxSpansPerRequest: maximumMaxSpans + 1,
	}); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("invalid span limit error = %v", err)
	}
}

func TestAcceptsOTLPProtobufWithIdentityAndGzip(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{"identity", "gzip"} {
		encoding := encoding
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()
			writer := &recordingWriter{result: telemetry.WriteResult{Inserted: 1}}
			handler := mustHandler(t, writer, Options{})
			document := marshalExport(t, validExportRequest())
			if encoding == "gzip" {
				document = gzipDocument(t, document)
			}

			response := serve(handler, http.MethodPost, tracesPath, document, testIngestToken, protobufContentType, encoding)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != protobufContentType {
				t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			var exportResponse collectortracev1.ExportTraceServiceResponse
			if err := proto.Unmarshal(response.Body.Bytes(), &exportResponse); err != nil {
				t.Fatal(err)
			}
			if exportResponse.PartialSuccess != nil {
				t.Fatalf("unexpected partial success: %#v", exportResponse.PartialSuccess)
			}
			if writer.batchCount() != 1 || len(writer.batches[0].Spans) != 1 || len(writer.batches[0].Links) != 1 {
				t.Fatalf("stored batch = %#v", writer.batches)
			}
			batch := writer.batches[0]
			span := batch.Spans[0]
			if span.Resource["service.name"] != "research-agent" || span.Attributes["llm.model_name"] != "gpt-test" ||
				span.InstrumentationScope != "openinference.instrumentation.langchain" || len(span.Events) != 1 ||
				span.Events[0].Attributes["event.kind"] != "diagnostic" || batch.Links[0].Attributes["link.kind"] != "async" {
				t.Fatalf("resource/scope/span/event/link normalization was incomplete: %#v", batch)
			}
		})
	}
}

func TestWritesMultipleAcceptedSpansAsOneBatch(t *testing.T) {
	t.Parallel()

	request := validExportRequest()
	second := proto.Clone(request.ResourceSpans[0].ScopeSpans[0].Spans[0]).(*tracev1.Span)
	second.SpanId = []byte{8, 7, 6, 5, 4, 3, 2, 9}
	second.ParentSpanId = append([]byte(nil), request.ResourceSpans[0].ScopeSpans[0].Spans[0].SpanId...)
	request.ResourceSpans[0].ScopeSpans[0].Spans = append(request.ResourceSpans[0].ScopeSpans[0].Spans, second)
	writer := &recordingWriter{result: telemetry.WriteResult{Inserted: 2}}
	handler := mustHandler(t, writer, Options{})

	response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, request), testIngestToken, protobufContentType, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
	}
	if writer.batchCount() != 1 || len(writer.batches[0].Spans) != 2 {
		t.Fatalf("writer calls=%d batch=%#v", writer.batchCount(), writer.batches)
	}
}

func TestRejectsRequestAboveConfiguredSpanBatchLimit(t *testing.T) {
	t.Parallel()

	request := validExportRequest()
	second := proto.Clone(request.ResourceSpans[0].ScopeSpans[0].Spans[0]).(*tracev1.Span)
	second.SpanId = []byte{8, 7, 6, 5, 4, 3, 2, 9}
	request.ResourceSpans[0].ScopeSpans[0].Spans = append(
		request.ResourceSpans[0].ScopeSpans[0].Spans,
		second,
	)
	writer := &recordingWriter{}
	handler := mustHandler(t, writer, Options{MaxSpansPerRequest: 1})
	response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, request), testIngestToken, protobufContentType, "")
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "too many spans") {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if writer.batchCount() != 0 {
		t.Fatalf("writer received %d batches", writer.batchCount())
	}
}

func TestRejectsInvalidHTTPAndProtobufRequestsWithoutWriting(t *testing.T) {
	t.Parallel()

	validBody := marshalExport(t, validExportRequest())
	tests := []struct {
		name        string
		method      string
		path        string
		body        []byte
		token       string
		contentType string
		encoding    string
		status      int
	}{
		{name: "path", method: http.MethodPost, path: "/v1/metrics", body: validBody, token: testIngestToken, contentType: protobufContentType, status: http.StatusNotFound},
		{name: "method", method: http.MethodPut, path: tracesPath, body: validBody, token: testIngestToken, contentType: protobufContentType, status: http.StatusMethodNotAllowed},
		{name: "missing auth", method: http.MethodPost, path: tracesPath, body: validBody, contentType: protobufContentType, status: http.StatusUnauthorized},
		{name: "wrong auth", method: http.MethodPost, path: tracesPath, body: validBody, token: "wrong-token-with-enough-bytes", contentType: protobufContentType, status: http.StatusUnauthorized},
		{name: "content type", method: http.MethodPost, path: tracesPath, body: validBody, token: testIngestToken, contentType: "application/json", status: http.StatusUnsupportedMediaType},
		{name: "content type parameter", method: http.MethodPost, path: tracesPath, body: validBody, token: testIngestToken, contentType: protobufContentType + "; charset=utf-8", status: http.StatusUnsupportedMediaType},
		{name: "encoding", method: http.MethodPost, path: tracesPath, body: validBody, token: testIngestToken, contentType: protobufContentType, encoding: "br", status: http.StatusUnsupportedMediaType},
		{name: "protobuf", method: http.MethodPost, path: tracesPath, body: []byte{0xff}, token: testIngestToken, contentType: protobufContentType, status: http.StatusBadRequest},
		{name: "gzip", method: http.MethodPost, path: tracesPath, body: []byte("not gzip"), token: testIngestToken, contentType: protobufContentType, encoding: "gzip", status: http.StatusBadRequest},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := &recordingWriter{}
			handler := mustHandler(t, writer, Options{})
			response := serve(handler, test.method, test.path, test.body, test.token, test.contentType, test.encoding)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.status, response.Body.String())
			}
			if writer.batchCount() != 0 {
				t.Fatalf("writer received %d batches", writer.batchCount())
			}
		})
	}
}

func TestRejectsAmbiguousAuthorizationHeaders(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	handler := mustHandler(t, writer, Options{})
	request := httptest.NewRequest(http.MethodPost, tracesPath, bytes.NewReader(marshalExport(t, validExportRequest())))
	request.Header.Set("Content-Type", protobufContentType)
	request.Header.Add("Authorization", "Bearer "+testIngestToken)
	request.Header.Add("Authorization", "Bearer "+testIngestToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != `Bearer realm="agentshark-traces"` ||
		strings.Contains(response.Body.String(), testIngestToken) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if writer.batchCount() != 0 {
		t.Fatalf("writer received %d batches", writer.batchCount())
	}
}

func TestRejectsCompressedAndDecompressedSizeOverruns(t *testing.T) {
	t.Parallel()

	t.Run("compressed", func(t *testing.T) {
		writer := &recordingWriter{}
		handler := mustHandler(t, writer, Options{MaxCompressedBytes: 32, MaxDecompressedBytes: 128})
		request := httptest.NewRequest(http.MethodPost, tracesPath, bytes.NewReader(bytes.Repeat([]byte{1}, 33)))
		request.ContentLength = -1
		request.Header.Set("Authorization", "Bearer "+testIngestToken)
		request.Header.Set("Content-Type", protobufContentType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "compressed") {
			t.Fatalf("response = %d %q", response.Code, response.Body.String())
		}
	})

	t.Run("decompressed", func(t *testing.T) {
		writer := &recordingWriter{}
		handler := mustHandler(t, writer, Options{MaxCompressedBytes: 1024, MaxDecompressedBytes: 1024})
		response := serve(handler, http.MethodPost, tracesPath, gzipDocument(t, bytes.Repeat([]byte{1}, 1025)), testIngestToken, protobufContentType, "gzip")
		if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "decompressed") {
			t.Fatalf("response = %d %q", response.Code, response.Body.String())
		}
	})
}

func TestPartiallyAcceptsValidSpansAndReturnsSafeDiagnostics(t *testing.T) {
	t.Parallel()

	request := validExportRequest()
	invalid := proto.Clone(request.ResourceSpans[0].ScopeSpans[0].Spans[0]).(*tracev1.Span)
	invalid.TraceId = make([]byte, 16)
	invalid.Name = "prompt-text-that-must-not-leak"
	request.ResourceSpans[0].ScopeSpans[0].Spans = append(request.ResourceSpans[0].ScopeSpans[0].Spans, invalid)
	writer := &recordingWriter{result: telemetry.WriteResult{Inserted: 1}}
	metrics := &Metrics{}
	handler := mustHandler(t, writer, Options{Metrics: metrics})

	response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, request), testIngestToken, protobufContentType, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
	}
	var exportResponse collectortracev1.ExportTraceServiceResponse
	if err := proto.Unmarshal(response.Body.Bytes(), &exportResponse); err != nil {
		t.Fatal(err)
	}
	partial := exportResponse.GetPartialSuccess()
	if partial.GetRejectedSpans() != 1 || !strings.Contains(partial.GetErrorMessage(), "invalid_trace_id=1") {
		t.Fatalf("partial success = %#v", partial)
	}
	if strings.Contains(partial.GetErrorMessage(), invalid.Name) || strings.Contains(partial.GetErrorMessage(), testIngestToken) {
		t.Fatalf("diagnostic leaked request content: %q", partial.GetErrorMessage())
	}
	if writer.batchCount() != 1 || len(writer.batches[0].Spans) != 1 {
		t.Fatalf("stored batches = %#v", writer.batches)
	}
	snapshot := metrics.Snapshot()
	if snapshot.SpansReceived != 2 || snapshot.SpansAccepted != 1 || snapshot.SpansRejected != 1 || snapshot.RequestRejections != 0 {
		t.Fatalf("metrics = %#v", snapshot)
	}
}

func TestRejectsInvalidW3CIdentifiersWithoutCallingStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*tracev1.Span)
		category string
	}{
		{name: "trace ID length", mutate: func(span *tracev1.Span) { span.TraceId = make([]byte, 15) }, category: "invalid_trace_id=1"},
		{name: "span ID zero", mutate: func(span *tracev1.Span) { span.SpanId = make([]byte, 8) }, category: "invalid_span_id=1"},
		{name: "parent span ID length", mutate: func(span *tracev1.Span) { span.ParentSpanId = make([]byte, 7) }, category: "invalid_parent_span_id=1"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validExportRequest()
			test.mutate(request.ResourceSpans[0].ScopeSpans[0].Spans[0])
			writer := &recordingWriter{}
			handler := mustHandler(t, writer, Options{})
			response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, request), testIngestToken, protobufContentType, "")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
			}
			var exportResponse collectortracev1.ExportTraceServiceResponse
			if err := proto.Unmarshal(response.Body.Bytes(), &exportResponse); err != nil {
				t.Fatal(err)
			}
			if partial := exportResponse.GetPartialSuccess(); partial.GetRejectedSpans() != 1 || !strings.Contains(partial.GetErrorMessage(), test.category) {
				t.Fatalf("partial success = %#v", partial)
			}
			if writer.batchCount() != 0 {
				t.Fatalf("writer received %d batches", writer.batchCount())
			}
		})
	}
}

func TestStorageFailureAndTimeoutDoNotLeakWriterErrors(t *testing.T) {
	t.Parallel()

	t.Run("failure", func(t *testing.T) {
		var logs bytes.Buffer
		writer := &recordingWriter{err: errors.New("prompt-text authorization-secret")}
		metrics := &Metrics{}
		handler := mustHandler(t, writer, Options{Metrics: metrics, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, validExportRequest()), testIngestToken, protobufContentType, "")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
		}
		combined := logs.String() + response.Body.String()
		for _, secret := range []string{"prompt-text", "authorization-secret", testIngestToken} {
			if strings.Contains(combined, secret) {
				t.Fatalf("failure output leaked %q: %s", secret, combined)
			}
		}
		if snapshot := metrics.Snapshot(); snapshot.Writes != 1 || snapshot.WriteFailures != 1 || snapshot.RequestRejections != 1 {
			t.Fatalf("metrics = %#v", snapshot)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		writer := &recordingWriter{write: func(ctx context.Context, _ telemetry.TraceBatch) (telemetry.WriteResult, error) {
			<-ctx.Done()
			return telemetry.WriteResult{}, ctx.Err()
		}}
		handler := mustHandler(t, writer, Options{RequestTimeout: 10 * time.Millisecond})
		response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, validExportRequest()), testIngestToken, protobufContentType, "")
		if response.Code != http.StatusGatewayTimeout {
			t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
		}
	})
}

func TestRejectsBodyThatExceedsTheRequestDeadline(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	handler := mustHandler(t, writer, Options{RequestTimeout: 5 * time.Millisecond})
	request := httptest.NewRequest(http.MethodPost, tracesPath, &delayedReader{
		delay: 20 * time.Millisecond, data: marshalExport(t, validExportRequest()),
	})
	request.Header.Set("Authorization", "Bearer "+testIngestToken)
	request.Header.Set("Content-Type", protobufContentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d body=%q", response.Code, response.Body.String())
	}
	if writer.batchCount() != 0 {
		t.Fatalf("writer received %d batches", writer.batchCount())
	}
}

func TestMetricsRecordDuplicateAndWriteLatency(t *testing.T) {
	t.Parallel()

	metrics := &Metrics{}
	writer := &recordingWriter{result: telemetry.WriteResult{Updated: 1, Duplicates: 2}}
	handler := mustHandler(t, writer, Options{Metrics: metrics})
	response := serve(handler, http.MethodPost, tracesPath, marshalExport(t, validExportRequest()), testIngestToken, protobufContentType, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	snapshot := metrics.Snapshot()
	if snapshot.Requests != 1 || snapshot.Writes != 1 || snapshot.SpansUpdated != 1 || snapshot.SpansDuplicated != 2 {
		t.Fatalf("metrics = %#v", snapshot)
	}
	if snapshot.WriteLatencyLast < 0 || snapshot.WriteLatencyTotal < snapshot.WriteLatencyLast || snapshot.WriteLatencyMax < snapshot.WriteLatencyLast {
		t.Fatalf("write latency metrics = %#v", snapshot)
	}
}

func mustHandler(t *testing.T, writer *recordingWriter, options Options) *Handler {
	t.Helper()
	options.IngestToken = testIngestToken
	handler, err := New(writer, options)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func serve(handler http.Handler, method, path string, body []byte, token, contentType, encoding string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if encoding != "" && encoding != "identity" {
		request.Header.Set("Content-Encoding", encoding)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func marshalExport(t *testing.T, request *collectortracev1.ExportTraceServiceRequest) []byte {
	t.Helper()
	document, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func gzipDocument(t *testing.T, document []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(document); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func validExportRequest() *collectortracev1.ExportTraceServiceRequest {
	startedAt := uint64(time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC).UnixNano())
	return &collectortracev1.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
		Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{
			{Key: "service.name", Value: stringValue("research-agent")},
			{Key: "agentshark.agent.id", Value: stringValue("agent-1")},
		}},
		ScopeSpans: []*tracev1.ScopeSpans{{
			Scope: &commonv1.InstrumentationScope{Name: "openinference.instrumentation.langchain", Version: "1.0.0"},
			Spans: []*tracev1.Span{{
				TraceId: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				SpanId:  []byte{1, 2, 3, 4, 5, 6, 7, 8}, Name: "ChatModel.invoke", Kind: tracev1.Span_SPAN_KIND_CLIENT,
				StartTimeUnixNano: startedAt, EndTimeUnixNano: startedAt + uint64(time.Second),
				Attributes: []*commonv1.KeyValue{
					{Key: "openinference.span.kind", Value: stringValue("LLM")},
					{Key: "llm.model_name", Value: stringValue("gpt-test")},
				},
				Events: []*tracev1.Span_Event{{
					TimeUnixNano: startedAt + uint64(time.Millisecond), Name: "model.started",
					Attributes: []*commonv1.KeyValue{{Key: "event.kind", Value: stringValue("diagnostic")}},
				}},
				Links: []*tracev1.Span_Link{{
					TraceId:    []byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
					SpanId:     []byte{8, 7, 6, 5, 4, 3, 2, 1},
					Attributes: []*commonv1.KeyValue{{Key: "link.kind", Value: stringValue("async")}},
				}},
				Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
			}},
		}},
	}}}
}

func stringValue(value string) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}
}
