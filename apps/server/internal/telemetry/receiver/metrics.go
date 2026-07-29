// Package receiver implements the bounded OTLP/HTTP trace ingest endpoint.
package receiver

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics tracks collector ingest outcomes without retaining request content.
// All values are process-local and safe to read while requests are active.
type Metrics struct {
	requests          atomic.Uint64
	requestRejections atomic.Uint64
	spansReceived     atomic.Uint64
	spansAccepted     atomic.Uint64
	spansRejected     atomic.Uint64
	spansInserted     atomic.Uint64
	spansUpdated      atomic.Uint64
	spansDuplicated   atomic.Uint64
	writes            atomic.Uint64
	writeFailures     atomic.Uint64
	writeLatencyNanos atomic.Uint64
	lastWriteNanos    atomic.Uint64
	maxWriteNanos     atomic.Uint64
}

// MetricsSnapshot is one internally consistent-enough point-in-time view of
// monotonic counters. It never contains trace attributes or payload content.
type MetricsSnapshot struct {
	Requests          uint64
	RequestRejections uint64
	SpansReceived     uint64
	SpansAccepted     uint64
	SpansRejected     uint64
	SpansInserted     uint64
	SpansUpdated      uint64
	SpansDuplicated   uint64
	Writes            uint64
	WriteFailures     uint64
	WriteLatencyTotal time.Duration
	WriteLatencyLast  time.Duration
	WriteLatencyMax   time.Duration
}

func (metrics *Metrics) Snapshot() MetricsSnapshot {
	if metrics == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		Requests: metrics.requests.Load(), RequestRejections: metrics.requestRejections.Load(),
		SpansReceived: metrics.spansReceived.Load(), SpansAccepted: metrics.spansAccepted.Load(),
		SpansRejected: metrics.spansRejected.Load(), SpansInserted: metrics.spansInserted.Load(),
		SpansUpdated: metrics.spansUpdated.Load(), SpansDuplicated: metrics.spansDuplicated.Load(),
		Writes: metrics.writes.Load(), WriteFailures: metrics.writeFailures.Load(),
		WriteLatencyTotal: time.Duration(metrics.writeLatencyNanos.Load()),
		WriteLatencyLast:  time.Duration(metrics.lastWriteNanos.Load()),
		WriteLatencyMax:   time.Duration(metrics.maxWriteNanos.Load()),
	}
}

// PrometheusHandler exposes only aggregate process-local counters and
// durations. It never includes labels derived from Trace data.
func (metrics *Metrics) PrometheusHandler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snapshot := metrics.Snapshot()
		response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		writeCounter(response, "agentshark_collector_requests_total", snapshot.Requests)
		writeCounter(response, "agentshark_collector_request_rejections_total", snapshot.RequestRejections)
		writeCounter(response, "agentshark_collector_spans_received_total", snapshot.SpansReceived)
		writeCounter(response, "agentshark_collector_spans_accepted_total", snapshot.SpansAccepted)
		writeCounter(response, "agentshark_collector_spans_rejected_total", snapshot.SpansRejected)
		writeCounter(response, "agentshark_collector_spans_inserted_total", snapshot.SpansInserted)
		writeCounter(response, "agentshark_collector_spans_updated_total", snapshot.SpansUpdated)
		writeCounter(response, "agentshark_collector_spans_duplicated_total", snapshot.SpansDuplicated)
		writeCounter(response, "agentshark_collector_writes_total", snapshot.Writes)
		writeCounter(response, "agentshark_collector_write_failures_total", snapshot.WriteFailures)
		_, _ = fmt.Fprintf(response, "# TYPE agentshark_collector_write_latency_seconds summary\n")
		_, _ = fmt.Fprintf(response, "agentshark_collector_write_latency_seconds_sum %g\n", snapshot.WriteLatencyTotal.Seconds())
		_, _ = fmt.Fprintf(response, "agentshark_collector_write_latency_seconds_count %d\n", snapshot.Writes)
		_, _ = fmt.Fprintf(response, "# TYPE agentshark_collector_write_latency_last_seconds gauge\n")
		_, _ = fmt.Fprintf(response, "agentshark_collector_write_latency_last_seconds %g\n", snapshot.WriteLatencyLast.Seconds())
		_, _ = fmt.Fprintf(response, "# TYPE agentshark_collector_write_latency_max_seconds gauge\n")
		_, _ = fmt.Fprintf(response, "agentshark_collector_write_latency_max_seconds %g\n", snapshot.WriteLatencyMax.Seconds())
	})
}

func writeCounter(response http.ResponseWriter, name string, value uint64) {
	_, _ = fmt.Fprintf(response, "# TYPE %s counter\n%s %d\n", name, name, value)
}

func (metrics *Metrics) rejectRequest() {
	metrics.requestRejections.Add(1)
}

func (metrics *Metrics) observeNormalization(received, accepted, rejected int) {
	addPositive(&metrics.spansReceived, received)
	addPositive(&metrics.spansAccepted, accepted)
	addPositive(&metrics.spansRejected, rejected)
}

func (metrics *Metrics) observeWrite(startedAt time.Time, inserted, updated, duplicated int, failed bool) {
	elapsed := time.Since(startedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	nanos := uint64(elapsed)
	metrics.writes.Add(1)
	metrics.writeLatencyNanos.Add(nanos)
	metrics.lastWriteNanos.Store(nanos)
	for maximum := metrics.maxWriteNanos.Load(); nanos > maximum && !metrics.maxWriteNanos.CompareAndSwap(maximum, nanos); maximum = metrics.maxWriteNanos.Load() {
	}
	if failed {
		metrics.writeFailures.Add(1)
		return
	}
	addPositive(&metrics.spansInserted, inserted)
	addPositive(&metrics.spansUpdated, updated)
	addPositive(&metrics.spansDuplicated, duplicated)
}

func addPositive(counter *atomic.Uint64, value int) {
	if value > 0 {
		counter.Add(uint64(value))
	}
}
