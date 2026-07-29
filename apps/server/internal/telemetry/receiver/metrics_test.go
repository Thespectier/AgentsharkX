package receiver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrometheusMetricsContainOnlyAggregateValues(t *testing.T) {
	t.Parallel()

	metrics := &Metrics{}
	metrics.requests.Add(2)
	metrics.observeNormalization(3, 2, 1)
	metrics.observeWrite(time.Now().Add(-time.Millisecond), 1, 0, 1, false)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.PrometheusHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{
		"agentshark_collector_requests_total 2",
		"agentshark_collector_spans_received_total 3",
		"agentshark_collector_spans_rejected_total 1",
		"agentshark_collector_spans_duplicated_total 1",
		"agentshark_collector_write_latency_seconds_count 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"trace_id", "span_id", "authorization", "prompt"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("metrics exposed forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestPrometheusMetricsRejectNonGET(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	response := httptest.NewRecorder()
	new(Metrics).PrometheusHandler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}
