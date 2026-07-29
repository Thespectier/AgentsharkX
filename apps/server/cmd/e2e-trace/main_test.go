package main

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestSendTraceUsesAuthenticatedOTLPProtobuf(t *testing.T) {
	const token = "release-trace-token-for-test"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("unexpected request: %s %q", request.Method, request.Header.Get("Content-Type"))
		}
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected Authorization header")
		}
		var export collectortracev1.ExportTraceServiceRequest
		if err := proto.Unmarshal(readBody(t, request), &export); err != nil {
			t.Fatal(err)
		}
		resourceSpans := export.GetResourceSpans()
		if len(resourceSpans) != 1 || len(resourceSpans[0].GetScopeSpans()) != 1 || len(resourceSpans[0].GetScopeSpans()[0].GetSpans()) != 1 {
			t.Fatal("fixture must contain exactly one span")
		}
		span := resourceSpans[0].GetScopeSpans()[0].GetSpans()[0]
		if got := hex.EncodeToString(span.GetTraceId()); got != "11111111111111111111111111111111" {
			t.Fatalf("trace ID = %q", got)
		}
		document, err := proto.Marshal(&collectortracev1.ExportTraceServiceResponse{})
		if err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = response.Write(document)
	}))
	defer server.Close()

	if err := sendTrace(context.Background(), server.Client(), server.URL, token, time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func readBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	document, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
