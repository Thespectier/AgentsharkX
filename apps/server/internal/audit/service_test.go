package audit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
)

type fakeGateway struct {
	feed      model.AuditFeed
	analytics model.GatewayAnalytics
	err       error
}

func (fake fakeGateway) TrafficWindow(context.Context, int, model.TrendWindow) (model.AuditFeed, error) {
	return fake.feed, fake.err
}
func (fake fakeGateway) AnalyticsWindow(context.Context, model.TrendWindow) (model.GatewayAnalytics, error) {
	return fake.analytics, nil
}

type fakeGuard struct {
	traffic    model.AuditFeed
	audit      model.AuditFeed
	sessions   []model.AuditSession
	trafficErr error
}

func (fake fakeGuard) Traffic(context.Context, int) (model.AuditFeed, error) {
	return fake.traffic, fake.trafficErr
}
func (fake fakeGuard) Audit(context.Context, int) (model.AuditFeed, error) {
	return fake.audit, nil
}
func (fake fakeGuard) AuditSessions(context.Context) ([]model.AuditSession, error) {
	return fake.sessions, nil
}

func TestRefreshPreservesSourcesVerifiesExactIDsAndPublishesAfterSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	gatewayEvent := auditEvent(model.SourceAgentGateway, "gateway:one", now, "shared-trace", "")
	guardEvent := auditEvent(model.SourceAgentGuard, "guard:one", now.Add(time.Millisecond), "shared-trace", "session-a")
	guardEvent.Decision = "DENY"
	hub := stream.NewHubWithCapacity(1000)
	service := New(fakeGateway{
		feed:      model.AuditFeed{Status: "available", Events: []model.UnifiedEvent{gatewayEvent}, Traffic: []model.AuditTrafficRecord{{Timestamp: now, LatencyMS: 20}}},
		analytics: model.GatewayAnalytics{Status: "unavailable", Reason: "analytics unavailable", Buckets: []model.AnalyticsBucket{}},
	}, fakeGuard{
		trafficErr: errors.New("guard traffic down"),
		audit:      model.AuditFeed{Status: "available", Events: []model.UnifiedEvent{guardEvent}},
		sessions:   []model.AuditSession{{ID: "session-id", UpstreamID: "session-a", Source: model.SourceAgentGuard}},
	}, hub)

	snapshot := service.Refresh(t.Context())
	if !snapshot.Meta.Partial || len(snapshot.Data.Events) != 2 || len(snapshot.Data.Sessions) != 1 {
		t.Fatalf("unexpected partial snapshot: %#v", snapshot)
	}
	if snapshot.Data.Sessions[0].Events != 1 || snapshot.Data.Sessions[0].Denies != 1 {
		t.Fatalf("exact session counts were not applied: %#v", snapshot.Data.Sessions[0])
	}
	for _, event := range snapshot.Data.Events {
		if event.Correlation == nil || !event.Correlation.Verified {
			t.Fatalf("exact cross-source trace was not verified: %#v", event)
		}
		if event.Raw != nil {
			t.Fatalf("list projection included raw detail: %#v", event.Raw)
		}
	}
	detail, ok := service.Find(model.SourceAgentGateway, "one")
	if !ok || detail.Raw == nil {
		t.Fatalf("redacted detail not retained: %#v ok=%t", detail, ok)
	}
	_, replay, unsubscribe := hub.Subscribe(0)
	defer unsubscribe()
	if len(replay) != 2 || replay[0].Event.Raw != nil {
		t.Fatalf("unexpected SSE replay: %#v", replay)
	}
}

func TestMergeEventsBoundsFiveThousandAndNeverTimeCorrelates(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	incoming := make([]model.UnifiedEvent, 5000)
	for index := range incoming {
		incoming[index] = auditEvent(model.SourceAgentGateway, fmt.Sprintf("gateway:%d", index), now.Add(time.Duration(index)*time.Nanosecond), "", "")
	}
	verifySharedIdentifiers(incoming)
	merged, fresh := mergeEvents(nil, incoming, 1000)
	if len(merged) != 1000 || len(fresh) != 5000 || merged[0].ID != "gateway:4999" {
		t.Fatalf("unexpected bounded merge: merged=%d fresh=%d first=%s", len(merged), len(fresh), merged[0].ID)
	}
	for _, event := range merged {
		if event.Correlation != nil && event.Correlation.Verified {
			t.Fatal("events were correlated without an explicit shared identifier")
		}
	}
}

func TestRefreshPreservesEmptyArrayContract(t *testing.T) {
	t.Parallel()
	service := New(fakeGateway{}, fakeGuard{}, nil)

	snapshot := service.Refresh(t.Context())
	if snapshot.Data.Metrics == nil || snapshot.Data.Trend == nil || snapshot.Data.Events == nil || snapshot.Data.Sessions == nil {
		t.Fatalf("empty audit collections must serialize as arrays: %#v", snapshot.Data)
	}
}

func TestTrendUsesExactFiveMinuteBucketsAndNearestRankP95(t *testing.T) {
	t.Parallel()
	window := model.TrendWindow{
		From:           time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC),
		To:             time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		BucketDuration: model.TrendBucketDuration,
	}
	latencies := make([]model.AuditTrafficRecord, 0, 22)
	for value := 1; value <= 20; value++ {
		latencies = append(latencies, model.AuditTrafficRecord{
			Timestamp: window.From.Add(time.Minute), LatencyMS: float64(value),
		})
	}
	latencies = append(latencies,
		model.AuditTrafficRecord{Timestamp: window.From.Add(-time.Second), LatencyMS: 9999},
		model.AuditTrafficRecord{Timestamp: window.To, LatencyMS: 9999},
	)
	requests := int64(7)
	bucketSeconds := int64(model.TrendBucketDuration / time.Second)
	trend := buildTrend(window, struct {
		traffic      model.AuditFeed
		analytics    model.GatewayAnalytics
		trafficErr   error
		analyticsErr error
	}{
		traffic: model.AuditFeed{Traffic: latencies},
		analytics: model.GatewayAnalytics{
			Status: "available", Requests: &requests, BucketSeconds: &bucketSeconds,
			Buckets: []model.AnalyticsBucket{{Start: window.From, Requests: requests}},
		},
	}, struct {
		traffic     model.AuditFeed
		audit       model.AuditFeed
		sessions    []model.AuditSession
		trafficErr  error
		auditErr    error
		sessionsErr error
	}{
		traffic: model.AuditFeed{Traffic: []model.AuditTrafficRecord{
			{Timestamp: window.From.Add(2 * time.Minute), Action: "DENY"},
			{Timestamp: window.From.Add(-time.Second), Action: "DENY"},
		}},
	})

	if len(trend) != model.TrendBucketCount {
		t.Fatalf("expected %d points, got %d", model.TrendBucketCount, len(trend))
	}
	if trend[0].Time != "2026-07-24T08:00:00Z" || trend[0].Requests != 7 || trend[0].Denied != 1 {
		t.Fatalf("unexpected first bucket: %#v", trend[0])
	}
	if trend[0].Latency == nil || *trend[0].Latency != 19 || trend[0].LatencySamples != 20 {
		t.Fatalf("expected nearest-rank P95 19ms from 20 samples, got %#v", trend[0])
	}
	if trend[1].Time != "2026-07-24T08:05:00Z" || trend[1].Latency != nil || trend[1].LatencySamples != 0 {
		t.Fatalf("empty buckets must retain an exact timestamp and null latency: %#v", trend[1])
	}
}

func TestMetricsExcludeRecordsOutsideTrendWindow(t *testing.T) {
	t.Parallel()
	window := model.TrendWindow{
		From:           time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC),
		To:             time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		BucketDuration: model.TrendBucketDuration,
	}
	metrics := buildMetrics(window, struct {
		traffic      model.AuditFeed
		analytics    model.GatewayAnalytics
		trafficErr   error
		analyticsErr error
	}{
		traffic: model.AuditFeed{Traffic: []model.AuditTrafficRecord{
			{Timestamp: window.From.Add(time.Minute), LatencyMS: 25},
			{Timestamp: window.From.Add(-time.Second), LatencyMS: 9000},
		}},
	}, struct {
		traffic     model.AuditFeed
		audit       model.AuditFeed
		sessions    []model.AuditSession
		trafficErr  error
		auditErr    error
		sessionsErr error
	}{
		traffic: model.AuditFeed{Traffic: []model.AuditTrafficRecord{
			{Timestamp: window.From.Add(time.Minute), Action: "deny"},
			{Timestamp: window.From.Add(2 * time.Minute), Action: "allow"},
			{Timestamp: window.To, Action: "deny"},
		}},
	})

	if metrics[0].Value != 1 || metrics[1].Value != 25 || metrics[2].Value != 2 || metrics[3].Value != 50 {
		t.Fatalf("metrics were not constrained to the shared window: %#v", metrics)
	}
}

func auditEvent(source model.Source, id string, timestamp time.Time, traceID, sessionID string) model.UnifiedEvent {
	correlation := (*model.EventCorrelation)(nil)
	if traceID != "" || sessionID != "" {
		correlation = &model.EventCorrelation{TraceID: traceID, SessionID: sessionID}
	}
	return model.UnifiedEvent{
		ID: id, Timestamp: timestamp, Source: source, Kind: "audit", Severity: "info",
		Subject: &model.EventSubject{SessionID: sessionID}, Correlation: correlation,
		Summary: "event", RawRef: model.RawRef{Source: source, ID: id[len(id)-3:]},
		Raw: map[string]any{"safe": true},
	}
}
