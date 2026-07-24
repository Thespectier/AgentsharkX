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
	TrafficWindow(context.Context, int, model.TrendWindow) (model.AuditFeed, error)
	AnalyticsWindow(context.Context, model.TrendWindow) (model.GatewayAnalytics, error)
}

type Guard interface {
	Traffic(context.Context, int) (model.AuditFeed, error)
	Audit(context.Context, int) (model.AuditFeed, error)
	AuditSessions(context.Context) ([]model.AuditSession, error)
}

type Service struct {
	mu               sync.RWMutex
	gateway          Gateway
	guard            Guard
	stream           *stream.Hub
	data             model.AuditData
	meta             model.Meta
	resolutions      []approvalResolution
	resolutionKeys   map[string]struct{}
	lastWindow       model.TrendWindow
	lastGuardTraffic []model.AuditTrafficRecord
	lastGuardAudit   []model.UnifiedEvent
}

type approvalResolution struct {
	event           model.UnifiedEvent
	record          model.AuditTrafficRecord
	originalEventID string
}

func New(gateway Gateway, guard Guard, hub *stream.Hub) *Service {
	if hub == nil {
		hub = stream.NewHub()
	}
	return &Service{
		gateway: gateway, guard: guard, stream: hub,
		data:        model.AuditData{Metrics: []model.Metric{}, Trend: []model.TrendPoint{}, Events: []model.UnifiedEvent{}, Sessions: []model.AuditSession{}},
		meta:        model.Meta{FetchedAt: time.Now().UTC(), SourceFailures: []model.SourceFailure{}},
		resolutions: []approvalResolution{}, resolutionKeys: make(map[string]struct{}),
	}
}

func (service *Service) Refresh(ctx context.Context) model.AuditEnvelope {
	window := model.CurrentTrendWindow(time.Now())
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
		gatewayResultValue.traffic, gatewayResultValue.trafficErr = service.gateway.TrafficWindow(ctx, fetchLimit, window)
	}()
	go func() {
		defer wait.Done()
		gatewayResultValue.analytics, gatewayResultValue.analyticsErr = service.gateway.AnalyticsWindow(ctx, window)
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

	service.mu.Lock()
	previous := cloneData(service.data)
	resolutions := append([]approvalResolution(nil), service.resolutions...)
	for _, resolution := range resolutions {
		incoming = append(incoming, resolution.event)
	}
	verifySharedIdentifiers(incoming)
	merged, fresh := mergeEvents(previous.Events, incoming, eventCapacity)
	sessions := append([]model.AuditSession{}, guardResultValue.sessions...)
	applySessionCounts(sessions, merged)
	metrics := buildMetrics(window, gatewayResultValue, guardResultValue)
	applyApprovalResolutionMetrics(
		metrics,
		window,
		guardResultValue.traffic.Traffic,
		guardResultValue.audit.Events,
		resolutions,
	)
	applyMetricDeltas(previous.Metrics, metrics)
	trend := buildTrend(window, gatewayResultValue, guardResultValue)
	applyApprovalResolutionTrend(trend, window, resolutions)
	meta := model.Meta{
		FetchedAt: time.Now().UTC(), Partial: len(failures) > 0, SourceFailures: failures,
	}
	data := model.AuditData{Metrics: metrics, Trend: trend, Events: merged, Sessions: sessions}
	service.data = cloneData(data)
	service.meta = cloneMeta(meta)
	service.lastWindow = window
	service.lastGuardTraffic = append([]model.AuditTrafficRecord(nil), guardResultValue.traffic.Traffic...)
	service.lastGuardAudit = append([]model.UnifiedEvent(nil), guardResultValue.audit.Events...)
	service.mu.Unlock()

	sort.Slice(fresh, func(i, j int) bool { return fresh[i].Timestamp.Before(fresh[j].Timestamp) })
	for _, event := range fresh {
		service.stream.Publish(withoutRaw(event))
	}
	return service.Snapshot()
}

// RecordApprovalResolution retains the confirmed management-plane outcome of
// an AgentGuard approval. AgentGuard's audit feed records the initial
// HUMAN_CHECK decision but its resolve endpoint returns only an acknowledgement,
// so this source-labelled evidence closes that otherwise invisible transition.
func (service *Service) RecordApprovalResolution(approval model.Approval, decision string, completedAt time.Time) {
	resolution, ok := newApprovalResolution(approval, decision, completedAt)
	if !ok {
		return
	}
	key := eventKey(resolution.event)
	service.mu.Lock()
	if _, exists := service.resolutionKeys[key]; exists {
		service.mu.Unlock()
		return
	}
	service.resolutionKeys[key] = struct{}{}
	service.resolutions = append(service.resolutions, resolution)
	if len(service.resolutions) > eventCapacity {
		service.resolutions = service.resolutions[len(service.resolutions)-eventCapacity:]
		service.rebuildResolutionKeys()
	}
	merged, fresh := mergeEvents(service.data.Events, []model.UnifiedEvent{resolution.event}, eventCapacity)
	service.data.Events = merged
	if approval.SessionID != "" {
		for index := range service.data.Sessions {
			if service.data.Sessions[index].UpstreamID != approval.SessionID {
				continue
			}
			service.data.Sessions[index].Events++
			if strings.EqualFold(decision, "deny") {
				service.data.Sessions[index].Denies++
			}
		}
	}
	window := service.lastWindow
	if window.From.IsZero() {
		window = model.CurrentTrendWindow(completedAt.Add(time.Second))
	}
	if !completedAt.Before(window.To) {
		window.To = completedAt.Add(time.Nanosecond)
	}
	applyApprovalResolutionMetrics(
		service.data.Metrics,
		window,
		service.lastGuardTraffic,
		service.lastGuardAudit,
		service.resolutions,
	)
	if strings.EqualFold(decision, "deny") {
		incrementDeniedTrend(service.data.Trend, window, completedAt)
	}
	service.meta.FetchedAt = completedAt.UTC()
	service.mu.Unlock()

	if len(fresh) == 1 {
		service.stream.Publish(withoutRaw(resolution.event))
	}
}

func (service *Service) rebuildResolutionKeys() {
	service.resolutionKeys = make(map[string]struct{}, len(service.resolutions))
	for _, resolution := range service.resolutions {
		service.resolutionKeys[eventKey(resolution.event)] = struct{}{}
	}
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

func buildMetrics(window model.TrendWindow, gatewayResult struct {
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
	gatewayTraffic := recordsInWindow(gatewayResult.traffic.Traffic, window)
	guardTraffic := recordsInWindow(guardResult.traffic.Traffic, window)
	requests := float64(len(gatewayTraffic))
	requestContext := "Last 60 minutes · verified request logs"
	if gatewayResult.analytics.Status == "available" && gatewayResult.analytics.Requests != nil {
		requests = float64(*gatewayResult.analytics.Requests)
		requestContext = "Last 60 minutes · gateway analytics"
	}
	latencies := make([]float64, 0, len(gatewayTraffic))
	for _, record := range gatewayTraffic {
		latencies = append(latencies, record.LatencyMS)
	}
	p95 := 0.0
	if value := nearestRankP95(latencies); value != nil {
		p95 = *value
	}
	denies := 0
	for _, record := range guardTraffic {
		if strings.EqualFold(record.Action, "deny") {
			denies++
		}
	}
	denyRate := 0.0
	if len(guardTraffic) > 0 {
		denyRate = float64(denies) * 100 / float64(len(guardTraffic))
	}
	return []model.Metric{
		{ID: "gateway-requests", Label: "Gateway requests", Source: model.SourceAgentGateway, Value: requests, Format: "integer", Trend: "flat", Tone: "default", Context: requestContext},
		{ID: "gateway-p95", Label: "P95 latency", Source: model.SourceAgentGateway, Value: p95, Format: "duration", Trend: "flat", Tone: "default", Context: "Last 60 minutes · nearest-rank P95"},
		{ID: "guard-decisions", Label: "Guard decisions", Source: model.SourceAgentGuard, Value: float64(len(guardTraffic)), Format: "integer", Trend: "flat", Tone: "default", Context: "Last 60 minutes · AgentGuard traffic"},
		{ID: "guard-deny-rate", Label: "Deny rate", Source: model.SourceAgentGuard, Value: denyRate, Format: "percent", Trend: "flat", Tone: metricTone(denyRate), Context: "Last 60 minutes · explicit DENY only"},
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

func applyApprovalResolutionMetrics(
	metrics []model.Metric,
	window model.TrendWindow,
	guardTraffic []model.AuditTrafficRecord,
	guardAudit []model.UnifiedEvent,
	resolutions []approvalResolution,
) {
	traffic := recordsInWindow(guardTraffic, window)
	denies := 0
	for _, record := range traffic {
		if strings.EqualFold(record.Action, "deny") {
			denies++
		}
	}
	auditIDs := make(map[string]struct{}, len(guardAudit))
	for _, event := range guardAudit {
		if !event.Timestamp.Before(window.From) && event.Timestamp.Before(window.To) {
			auditIDs[event.RawRef.ID] = struct{}{}
		}
	}
	resolutionOutcomes := make(map[string]string, len(resolutions))
	for _, resolution := range resolutions {
		if resolution.record.Timestamp.Before(window.From) || !resolution.record.Timestamp.Before(window.To) {
			continue
		}
		key := resolution.originalEventID
		if key == "" {
			key = resolution.event.RawRef.ID
		}
		if strings.EqualFold(resolution.record.Action, "deny") ||
			!strings.EqualFold(resolutionOutcomes[key], "deny") {
			resolutionOutcomes[key] = resolution.record.Action
		}
	}
	unmatchedResolutions := 0
	for eventID, action := range resolutionOutcomes {
		if _, matched := auditIDs[eventID]; !matched {
			unmatchedResolutions++
		}
		if strings.EqualFold(action, "deny") {
			denies++
		}
	}
	denominator := max(len(traffic)+unmatchedResolutions, denies)
	denyRate := 0.0
	if denominator > 0 {
		denyRate = float64(denies) * 100 / float64(denominator)
	}
	for index := range metrics {
		if metrics[index].ID != "guard-deny-rate" {
			continue
		}
		metrics[index].Value = denyRate
		metrics[index].Tone = metricTone(denyRate)
		metrics[index].Context = "Last 60 minutes · direct DENY and denied approvals"
	}
}

func applyApprovalResolutionTrend(
	trend []model.TrendPoint,
	window model.TrendWindow,
	resolutions []approvalResolution,
) {
	for _, resolution := range resolutions {
		if strings.EqualFold(resolution.record.Action, "deny") {
			incrementDeniedTrend(trend, window, resolution.record.Timestamp)
		}
	}
}

func incrementDeniedTrend(trend []model.TrendPoint, window model.TrendWindow, timestamp time.Time) {
	if len(trend) == 0 || timestamp.Before(window.From) || !timestamp.Before(window.To) {
		return
	}
	index := int(timestamp.Sub(window.From) / window.BucketDuration)
	if index == len(trend) && timestamp.Before(window.To) {
		index = len(trend) - 1
	}
	if index >= 0 && index < len(trend) {
		trend[index].Denied++
	}
}

func newApprovalResolution(
	approval model.Approval,
	decision string,
	completedAt time.Time,
) (approvalResolution, bool) {
	normalized := strings.ToLower(strings.TrimSpace(decision))
	if normalized != "approve" && normalized != "deny" {
		return approvalResolution{}, false
	}
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	} else {
		completedAt = completedAt.UTC()
	}
	action := strings.ToUpper(normalized)
	severity := "info"
	if normalized == "deny" {
		severity = "high"
	}
	phase := approval.Phase
	if phase == "" {
		phase = phaseForApprovalEvent(approval.EventType)
	}
	summaryTarget := approval.Tool
	if summaryTarget == "" {
		summaryTarget = strings.ReplaceAll(approval.EventType, "_", " ")
	}
	if summaryTarget == "" {
		summaryTarget = "guarded action"
	}
	summary := "Approval for " + summaryTarget + " was " + normalized + "d"
	if normalized == "deny" {
		summary = "Approval for " + summaryTarget + " was denied"
	}
	correlation := (*model.EventCorrelation)(nil)
	if approval.SessionID != "" {
		correlation = &model.EventCorrelation{SessionID: approval.SessionID, Verified: false}
	}
	event := model.UnifiedEvent{
		ID:        "guard:approval:" + approval.ID,
		Timestamp: completedAt,
		Source:    model.SourceAgentGuard,
		Kind:      "approval",
		Severity:  severity,
		Subject: &model.EventSubject{
			AgentID: approval.AgentUpstreamID, PrincipalID: approval.UserID, SessionID: approval.SessionID,
		},
		Target:      &model.EventTarget{Tool: approval.Tool},
		Phase:       phase,
		Action:      action,
		Decision:    action,
		Correlation: correlation,
		Summary:     summary,
		RawRef:      model.RawRef{Source: model.SourceAgentGuard, ID: approval.UpstreamID},
		Raw: map[string]any{
			"approval": map[string]any{
				"ticketId": approval.UpstreamID, "eventId": approval.EventID,
				"eventType": approval.EventType, "createdAt": approval.CreatedAt,
			},
			"decision": map[string]any{
				"action": action, "resolvedAt": completedAt, "riskScore": approval.RiskScore,
				"matchedRules": append([]string(nil), approval.MatchedRules...),
			},
			"redacted": []string{
				"operator note", "prompt", "payload", "authorization",
				"tool arguments", "tool result", "plugin result", "reason",
			},
		},
	}
	return approvalResolution{
		event:           event,
		originalEventID: approval.EventID,
		record: model.AuditTrafficRecord{
			Timestamp: completedAt,
			Action:    action,
			Risk:      approval.RiskScore,
		},
	}, true
}

func phaseForApprovalEvent(eventType string) string {
	switch strings.ToLower(eventType) {
	case "llm_input":
		return "llm_before"
	case "llm_output":
		return "llm_after"
	case "tool_invoke":
		return "tool_before"
	case "tool_result":
		return "tool_after"
	default:
		return ""
	}
}

func buildTrend(window model.TrendWindow, gatewayResult struct {
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
	duration := window.BucketDuration
	starts := make([]time.Time, model.TrendBucketCount)
	requests := make([]float64, model.TrendBucketCount)
	for index := range starts {
		starts[index] = window.From.UTC().Add(time.Duration(index) * duration)
	}
	usesAnalytics := gatewayResult.analyticsErr == nil && gatewayResult.analytics.Status == "available"
	if usesAnalytics {
		for _, bucket := range gatewayResult.analytics.Buckets {
			if index := bucketIndex(starts, duration, bucket.Start); index >= 0 {
				requests[index] = float64(bucket.Requests)
			}
		}
	}
	latencies := make([][]float64, model.TrendBucketCount)
	errors := make([]float64, model.TrendBucketCount)
	denied := make([]float64, model.TrendBucketCount)
	for _, record := range gatewayResult.traffic.Traffic {
		if index := bucketIndex(starts, duration, record.Timestamp); index >= 0 {
			if !usesAnalytics {
				requests[index]++
			}
			latencies[index] = append(latencies[index], record.LatencyMS)
			if strings.EqualFold(record.Action, "error") {
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
	result := make([]model.TrendPoint, model.TrendBucketCount)
	for index := range result {
		result[index] = model.TrendPoint{
			Time: starts[index].Format(time.RFC3339), Requests: requests[index],
			Latency: nearestRankP95(latencies[index]), LatencySamples: len(latencies[index]),
			Errors: errors[index], Denied: denied[index],
		}
	}
	return result
}

func recordsInWindow(records []model.AuditTrafficRecord, window model.TrendWindow) []model.AuditTrafficRecord {
	filtered := make([]model.AuditTrafficRecord, 0, len(records))
	for _, record := range records {
		if !record.Timestamp.Before(window.From) && record.Timestamp.Before(window.To) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func nearestRankP95(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := (95*len(sorted)+99)/100 - 1
	value := sorted[index]
	return &value
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
