// Package audit aggregates source-distinct gateway and guard evidence into a
// bounded management-plane view.
package audit

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
)

const (
	fetchLimit    = 500
	eventCapacity = stream.DefaultCapacity
)

var ErrInvalidCursor = errors.New("invalid audit cursor")

type Gateway interface {
	Traffic(context.Context, int) (model.AuditFeed, error)
	Analytics(context.Context) (model.GatewayAnalytics, error)
}

type Guard interface {
	Traffic(context.Context, int) (model.AuditFeed, error)
	Audit(context.Context, int) (model.AuditFeed, error)
	AuditSessions(context.Context) ([]model.AuditSession, error)
}

type Service struct {
	mu      sync.RWMutex
	gateway Gateway
	guard   Guard
	stream  *stream.Hub
	data    model.AuditData
	meta    model.Meta
}

func New(gateway Gateway, guard Guard, hub *stream.Hub) *Service {
	if hub == nil {
		hub = stream.NewHub()
	}
	return &Service{
		gateway: gateway, guard: guard, stream: hub,
		data: model.AuditData{Metrics: []model.Metric{}, Trend: []model.TrendPoint{}, Events: []model.UnifiedEvent{}, Sessions: []model.AuditSession{}},
		meta: model.Meta{FetchedAt: time.Now().UTC(), SourceFailures: []model.SourceFailure{}},
	}
}

func (service *Service) Refresh(ctx context.Context) model.AuditEnvelope {
	type gatewayResult struct {
		traffic      model.AuditFeed
		analytics    model.GatewayAnalytics
		trafficErr   error
		analyticsErr error
	}
	type guardResult struct {
		traffic     model.AuditFeed
		audit       model.AuditFeed
		sessions    []model.AuditSession
		trafficErr  error
		auditErr    error
		sessionsErr error
	}
	var gatewayResultValue gatewayResult
	var guardResultValue guardResult
	var wait sync.WaitGroup
	wait.Add(5)
	go func() {
		defer wait.Done()
		gatewayResultValue.traffic, gatewayResultValue.trafficErr = service.gateway.Traffic(ctx, fetchLimit)
	}()
	go func() {
		defer wait.Done()
		gatewayResultValue.analytics, gatewayResultValue.analyticsErr = service.gateway.Analytics(ctx)
	}()
	go func() {
		defer wait.Done()
		guardResultValue.traffic, guardResultValue.trafficErr = service.guard.Traffic(ctx, fetchLimit)
	}()
	go func() {
		defer wait.Done()
		guardResultValue.audit, guardResultValue.auditErr = service.guard.Audit(ctx, fetchLimit)
	}()
	go func() {
		defer wait.Done()
		guardResultValue.sessions, guardResultValue.sessionsErr = service.guard.AuditSessions(ctx)
	}()
	wait.Wait()

	failures := make([]model.SourceFailure, 0, 5)
	if gatewayResultValue.trafficErr != nil {
		failures = append(failures, failure(model.SourceAgentGateway, "gateway request-log search"))
	} else if gatewayResultValue.traffic.Status == "unavailable" {
		failures = append(failures, model.SourceFailure{Source: model.SourceAgentGateway, Code: "CAPABILITY_UNAVAILABLE", Message: gatewayResultValue.traffic.Reason})
	}
	if gatewayResultValue.analyticsErr != nil {
		failures = append(failures, failure(model.SourceAgentGateway, "gateway analytics"))
	} else if gatewayResultValue.analytics.Status == "unavailable" {
		failures = append(failures, model.SourceFailure{Source: model.SourceAgentGateway, Code: "CAPABILITY_UNAVAILABLE", Message: gatewayResultValue.analytics.Reason})
	}
	if guardResultValue.trafficErr != nil {
		failures = append(failures, failure(model.SourceAgentGuard, "AgentGuard traffic"))
	}
	if guardResultValue.auditErr != nil {
		failures = append(failures, failure(model.SourceAgentGuard, "AgentGuard audit"))
	}
	if guardResultValue.sessionsErr != nil {
		failures = append(failures, failure(model.SourceAgentGuard, "AgentGuard sessions"))
	}

	incoming := append([]model.UnifiedEvent{}, gatewayResultValue.traffic.Events...)
	incoming = append(incoming, guardResultValue.audit.Events...)
	verifySharedIdentifiers(incoming)

	service.mu.RLock()
	previous := cloneData(service.data)
	service.mu.RUnlock()
	merged, fresh := mergeEvents(previous.Events, incoming, eventCapacity)
	sessions := append([]model.AuditSession{}, guardResultValue.sessions...)
	applySessionCounts(sessions, merged)
	metrics := buildMetrics(gatewayResultValue, guardResultValue)
	applyMetricDeltas(previous.Metrics, metrics)
	trend := buildTrend(time.Now().UTC(), gatewayResultValue, guardResultValue)
	meta := model.Meta{
		FetchedAt: time.Now().UTC(), Partial: len(failures) > 0, SourceFailures: failures,
	}
	data := model.AuditData{Metrics: metrics, Trend: trend, Events: merged, Sessions: sessions}
	service.mu.Lock()
	service.data = cloneData(data)
	service.meta = cloneMeta(meta)
	service.mu.Unlock()

	sort.Slice(fresh, func(i, j int) bool { return fresh[i].Timestamp.Before(fresh[j].Timestamp) })
	for _, event := range fresh {
		service.stream.Publish(withoutRaw(event))
	}
	return service.Snapshot()
}

func (service *Service) Snapshot() model.AuditEnvelope {
	service.mu.RLock()
	defer service.mu.RUnlock()
	data := cloneData(service.data)
	for index := range data.Events {
		data.Events[index] = withoutRaw(data.Events[index])
	}
	return model.AuditEnvelope{Data: data, Meta: cloneMeta(service.meta)}
}

func (service *Service) OperationalSnapshot() model.OperationalSnapshot {
	snapshot := service.Snapshot()
	return model.OperationalSnapshot{
		Metrics: snapshot.Data.Metrics, Trend: snapshot.Data.Trend, Events: snapshot.Data.Events, Meta: snapshot.Meta,
	}
}

func (service *Service) Find(source model.Source, eventID string) (model.UnifiedEvent, bool) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	for _, event := range service.data.Events {
		if event.Source == source && (event.ID == eventID || event.RawRef.ID == eventID) {
			return event, true
		}
	}
	return model.UnifiedEvent{}, false
}

func (service *Service) Events(source model.Source, cursor string, limit int) (model.EventsEnvelope, error) {
	snapshot := service.Snapshot()
	items := make([]model.UnifiedEvent, 0, len(snapshot.Data.Events))
	for _, event := range snapshot.Data.Events {
		if source == "" || event.Source == source {
			items = append(items, event)
		}
	}
	offset := 0
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return model.EventsEnvelope{}, ErrInvalidCursor
		}
		offset, err = strconv.Atoi(string(decoded))
		if err != nil || offset < 0 || offset > len(items) {
			return model.EventsEnvelope{}, ErrInvalidCursor
		}
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	end := min(offset+limit, len(items))
	page := model.EventsPage{Items: append([]model.UnifiedEvent(nil), items[offset:end]...), Total: len(items)}
	if end < len(items) {
		next := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
		page.NextCursor = &next
	}
	return model.EventsEnvelope{Data: page, Meta: snapshot.Meta}, nil
}

func failure(source model.Source, capability string) model.SourceFailure {
	return model.SourceFailure{Source: source, Code: "UPSTREAM_UNAVAILABLE", Message: capability + " is unavailable"}
}

func verifySharedIdentifiers(events []model.UnifiedEvent) {
	type sources map[model.Source]struct{}
	traceSources := map[string]sources{}
	sessionSources := map[string]sources{}
	for _, event := range events {
		if event.Correlation == nil {
			continue
		}
		if id := strings.TrimSpace(event.Correlation.TraceID); id != "" {
			if traceSources[id] == nil {
				traceSources[id] = sources{}
			}
			traceSources[id][event.Source] = struct{}{}
		}
		if id := strings.TrimSpace(event.Correlation.SessionID); id != "" {
			if sessionSources[id] == nil {
				sessionSources[id] = sources{}
			}
			sessionSources[id][event.Source] = struct{}{}
		}
	}
	for index := range events {
		correlation := events[index].Correlation
		if correlation == nil {
			continue
		}
		correlation.Verified = (correlation.TraceID != "" && len(traceSources[correlation.TraceID]) > 1) ||
			(correlation.SessionID != "" && len(sessionSources[correlation.SessionID]) > 1)
	}
}

func mergeEvents(previous, incoming []model.UnifiedEvent, capacity int) ([]model.UnifiedEvent, []model.UnifiedEvent) {
	known := make(map[string]struct{}, len(previous))
	merged := append(make([]model.UnifiedEvent, 0, len(previous)+len(incoming)), previous...)
	for _, event := range previous {
		known[eventKey(event)] = struct{}{}
	}
	fresh := make([]model.UnifiedEvent, 0, len(incoming))
	for _, event := range incoming {
		if _, ok := known[eventKey(event)]; ok {
			continue
		}
		known[eventKey(event)] = struct{}{}
		merged = append(merged, event)
		fresh = append(fresh, event)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Timestamp.After(merged[j].Timestamp) })
	if len(merged) > capacity {
		merged = merged[:capacity]
	}
	return merged, fresh
}

func eventKey(event model.UnifiedEvent) string { return string(event.Source) + "\x00" + event.ID }

func applySessionCounts(sessions []model.AuditSession, events []model.UnifiedEvent) {
	bySession := make(map[string][]int, len(sessions))
	for index, session := range sessions {
		bySession[session.UpstreamID] = append(bySession[session.UpstreamID], index)
	}
	for _, event := range events {
		if event.Source != model.SourceAgentGuard || event.Subject == nil || event.Subject.SessionID == "" {
			continue
		}
		for _, index := range bySession[event.Subject.SessionID] {
			sessions[index].Events++
			if strings.EqualFold(event.Decision, "deny") {
				sessions[index].Denies++
			}
		}
	}
}

func buildMetrics(gatewayResult struct {
	traffic      model.AuditFeed
	analytics    model.GatewayAnalytics
	trafficErr   error
	analyticsErr error
}, guardResult struct {
	traffic     model.AuditFeed
	audit       model.AuditFeed
	sessions    []model.AuditSession
	trafficErr  error
	auditErr    error
	sessionsErr error
}) []model.Metric {
	requests := float64(len(gatewayResult.traffic.Traffic))
	requestContext := "Recent verified request-log records"
	if gatewayResult.analytics.Requests != nil {
		requests = float64(*gatewayResult.analytics.Requests)
		requestContext = "Gateway analytics window"
	}
	latencies := make([]float64, 0, len(gatewayResult.traffic.Traffic))
	for _, record := range gatewayResult.traffic.Traffic {
		latencies = append(latencies, record.LatencyMS)
	}
	sort.Float64s(latencies)
	p95 := 0.0
	if len(latencies) > 0 {
		index := (95*len(latencies)+99)/100 - 1
		if index < 0 {
			index = 0
		}
		p95 = latencies[index]
	}
	denies := 0
	for _, record := range guardResult.traffic.Traffic {
		if strings.EqualFold(record.Action, "deny") {
			denies++
		}
	}
	denyRate := 0.0
	if len(guardResult.traffic.Traffic) > 0 {
		denyRate = float64(denies) * 100 / float64(len(guardResult.traffic.Traffic))
	}
	return []model.Metric{
		{ID: "gateway-requests", Label: "Gateway requests", Source: model.SourceAgentGateway, Value: requests, Format: "integer", Trend: "flat", Tone: "default", Context: requestContext},
		{ID: "gateway-p95", Label: "P95 latency", Source: model.SourceAgentGateway, Value: p95, Format: "duration", Trend: "flat", Tone: "default", Context: "Recent request-log records"},
		{ID: "guard-decisions", Label: "Guard decisions", Source: model.SourceAgentGuard, Value: float64(len(guardResult.traffic.Traffic)), Format: "integer", Trend: "flat", Tone: "default", Context: "Recent AgentGuard traffic"},
		{ID: "guard-deny-rate", Label: "Deny rate", Source: model.SourceAgentGuard, Value: denyRate, Format: "percent", Trend: "flat", Tone: metricTone(denyRate), Context: "Explicit DENY decisions only"},
	}
}

func applyMetricDeltas(previous, current []model.Metric) {
	values := make(map[string]float64, len(previous))
	for _, metric := range previous {
		values[metric.ID] = metric.Value
	}
	for index := range current {
		old, ok := values[current[index].ID]
		if !ok {
			continue
		}
		change := current[index].Value - old
		if old != 0 {
			current[index].Delta = change * 100 / old
		} else if change != 0 {
			current[index].Delta = 100
		}
		if current[index].Delta > 0 {
			current[index].Trend = "up"
		} else if current[index].Delta < 0 {
			current[index].Trend = "down"
		}
	}
}

func metricTone(denyRate float64) string {
	if denyRate >= 20 {
		return "danger"
	}
	if denyRate > 0 {
		return "warning"
	}
	return "success"
}

func buildTrend(now time.Time, gatewayResult struct {
	traffic      model.AuditFeed
	analytics    model.GatewayAnalytics
	trafficErr   error
	analyticsErr error
}, guardResult struct {
	traffic     model.AuditFeed
	audit       model.AuditFeed
	sessions    []model.AuditSession
	trafficErr  error
	auditErr    error
	sessionsErr error
}) []model.TrendPoint {
	const count = 12
	duration := 5 * time.Minute
	if gatewayResult.analytics.BucketSeconds != nil && *gatewayResult.analytics.BucketSeconds > 0 {
		duration = time.Duration(*gatewayResult.analytics.BucketSeconds) * time.Second
	}
	starts := make([]time.Time, count)
	requests := make([]float64, count)
	usesAnalyticsBuckets := len(gatewayResult.analytics.Buckets) > 0
	if len(gatewayResult.analytics.Buckets) > 0 {
		buckets := gatewayResult.analytics.Buckets
		if len(buckets) > count {
			buckets = buckets[len(buckets)-count:]
		}
		padding := count - len(buckets)
		for index, bucket := range buckets {
			starts[padding+index] = bucket.Start.UTC()
			requests[padding+index] = float64(bucket.Requests)
		}
		for index := padding - 1; index >= 0; index-- {
			starts[index] = starts[index+1].Add(-duration)
		}
	} else {
		end := now.Truncate(duration)
		for index := range starts {
			starts[index] = end.Add(-time.Duration(count-1-index) * duration)
		}
	}
	latencySums := make([]float64, count)
	latencyCounts := make([]int, count)
	errors := make([]float64, count)
	denied := make([]float64, count)
	for _, record := range gatewayResult.traffic.Traffic {
		if index := bucketIndex(starts, duration, record.Timestamp); index >= 0 {
			if !usesAnalyticsBuckets {
				requests[index]++
			}
			latencySums[index] += record.LatencyMS
			latencyCounts[index]++
			if record.Action == "ERROR" {
				errors[index]++
			}
		}
	}
	for _, record := range guardResult.traffic.Traffic {
		if strings.EqualFold(record.Action, "deny") {
			if index := bucketIndex(starts, duration, record.Timestamp); index >= 0 {
				denied[index]++
			}
		}
	}
	result := make([]model.TrendPoint, count)
	for index := range result {
		latency := 0.0
		if latencyCounts[index] > 0 {
			latency = latencySums[index] / float64(latencyCounts[index])
		}
		result[index] = model.TrendPoint{Time: starts[index].Format("15:04"), Requests: requests[index], Latency: latency, Errors: errors[index], Denied: denied[index]}
	}
	return result
}

func bucketIndex(starts []time.Time, duration time.Duration, timestamp time.Time) int {
	for index := len(starts) - 1; index >= 0; index-- {
		if !timestamp.Before(starts[index]) && timestamp.Before(starts[index].Add(duration)) {
			return index
		}
	}
	return -1
}

func cloneData(data model.AuditData) model.AuditData {
	data.Metrics = append([]model.Metric{}, data.Metrics...)
	data.Trend = append([]model.TrendPoint{}, data.Trend...)
	data.Events = append([]model.UnifiedEvent{}, data.Events...)
	data.Sessions = append([]model.AuditSession{}, data.Sessions...)
	return data
}

func cloneMeta(meta model.Meta) model.Meta {
	meta.SourceFailures = append([]model.SourceFailure(nil), meta.SourceFailures...)
	return meta
}

func withoutRaw(event model.UnifiedEvent) model.UnifiedEvent {
	event.Raw = nil
	return event
}
