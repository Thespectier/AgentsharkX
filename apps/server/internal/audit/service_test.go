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

func (fake fakeGateway) Traffic(context.Context, int) (model.AuditFeed, error) {
	return fake.feed, fake.err
}
func (fake fakeGateway) Analytics(context.Context) (model.GatewayAnalytics, error) {
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
