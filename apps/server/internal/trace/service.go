// Package trace exposes authenticated, payload-safe Trace queries over the
// Collector-owned persistence records.
package trace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

var (
	ErrInvalidCursor      = errors.New("invalid trace cursor")
	ErrInvalidRequest     = errors.New("invalid trace query")
	ErrNotFound           = errors.New("trace or span was not found")
	ErrStorageUnavailable = errors.New("trace storage is unavailable")
)

const coverageSource = "agentshark-collector"

type Store interface {
	ListTraceSummaries(context.Context, storage.TraceFilter) (storage.Page[telemetry.Summary], error)
	GetTraceDetail(context.Context, string, storage.TraceGraphLimits) (storage.TraceDetail, error)
	GetTraceSpanDetail(context.Context, string, string) (storage.TraceSpanDetail, error)
}

type Filter = storage.TraceFilter

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) List(ctx context.Context, filter Filter) (model.TraceListEnvelope, error) {
	filter.Cursor = strings.TrimSpace(filter.Cursor)
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	filter.Completeness = strings.ToLower(strings.TrimSpace(filter.Completeness))
	filter.AgentID = strings.TrimSpace(filter.AgentID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.TaskID = strings.TrimSpace(filter.TaskID)
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if !validFilter(filter) {
		return model.TraceListEnvelope{}, ErrInvalidRequest
	}
	if service == nil || service.store == nil {
		return model.TraceListEnvelope{}, ErrStorageUnavailable
	}
	page, err := service.store.ListTraceSummaries(ctx, filter)
	if errors.Is(err, storage.ErrInvalidTraceCursor) {
		return model.TraceListEnvelope{}, ErrInvalidCursor
	}
	if err != nil {
		return model.TraceListEnvelope{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if page.Items == nil {
		page.Items = []telemetry.Summary{}
	}
	return model.TraceListEnvelope{
		Data: model.TraceListPage{Items: page.Items, NextCursor: page.NextCursor, Total: page.Total},
		Meta: service.meta(),
	}, nil
}

func (service *Service) Detail(ctx context.Context, traceID string) (model.TraceDetailEnvelope, error) {
	traceID = strings.ToLower(strings.TrimSpace(traceID))
	if !telemetry.ValidTraceID(traceID) {
		return model.TraceDetailEnvelope{}, ErrNotFound
	}
	if service == nil || service.store == nil {
		return model.TraceDetailEnvelope{}, ErrStorageUnavailable
	}
	detail, err := service.store.GetTraceDetail(ctx, traceID, storage.TraceGraphLimits{})
	if err != nil {
		return model.TraceDetailEnvelope{}, traceReadError(err)
	}
	summary, graph := detail.Summary, detail.Graph

	spans := make([]model.TraceSpan, 0, len(graph.Spans))
	coverageSpans := graph.Spans
	var root *model.TraceSpan
	if detail.RootSpan != nil {
		rootValue := projectSpan(*detail.RootSpan)
		root = &rootValue
	}
	rootVisible := false
	for _, span := range graph.Spans {
		projected := projectSpan(span)
		spans = append(spans, projected)
		if root != nil && span.SpanID == root.SpanID {
			rootVisible = true
		}
	}
	if root != nil && !rootVisible {
		spans = append(spans, *root)
		sort.Slice(spans, func(left, right int) bool {
			if spans[left].StartedAt.Equal(spans[right].StartedAt) {
				return spans[left].SpanID < spans[right].SpanID
			}
			return spans[left].StartedAt.Before(spans[right].StartedAt)
		})
		coverageSpans = append(append([]telemetry.Span(nil), graph.Spans...), *detail.RootSpan)
	}
	links := make([]model.TraceLink, 0, len(graph.Links))
	for _, link := range graph.Links {
		links = append(links, model.TraceLink{
			TraceID: link.TraceID, SpanID: link.SpanID, LinkedTraceID: link.LinkedTraceID,
			LinkedSpanID: link.LinkedSpanID,
		})
	}
	return model.TraceDetailEnvelope{
		Data: model.TraceDetail{
			Summary: summary, RootSpan: root, Spans: spans, Links: links,
			Coverage: coverage(coverageSpans), TotalSpans: graph.TotalSpans, TotalLinks: graph.TotalLinks,
			SpansTruncated: len(spans) < graph.TotalSpans, LinksTruncated: graph.LinksTruncated,
		},
		Meta: service.meta(),
	}, nil
}

func (service *Service) SpanDetail(ctx context.Context, traceID, spanID string) (model.TraceSpanDetailEnvelope, error) {
	traceID = strings.ToLower(strings.TrimSpace(traceID))
	spanID = strings.ToLower(strings.TrimSpace(spanID))
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return model.TraceSpanDetailEnvelope{}, ErrNotFound
	}
	if service == nil || service.store == nil {
		return model.TraceSpanDetailEnvelope{}, ErrStorageUnavailable
	}
	detail, err := service.store.GetTraceSpanDetail(ctx, traceID, spanID)
	if err != nil {
		return model.TraceSpanDetailEnvelope{}, traceReadError(err)
	}
	span, payloads := detail.Span, detail.Payloads
	events := make([]model.TraceEvent, 0, len(span.Events))
	for _, event := range span.Events {
		if event.Attributes == nil {
			event.Attributes = map[string]any{}
		}
		events = append(events, model.TraceEvent{
			Name: event.Name, Time: event.Time, Attributes: event.Attributes,
			DroppedAttributesCount: event.DroppedAttributesCount,
		})
	}
	payloadDetails := make([]model.TracePayload, 0, len(payloads))
	for _, payload := range payloads {
		payloadDetails = append(payloadDetails, model.TracePayload{
			Kind: payload.Kind, ContentType: payload.ContentType, Encoding: payload.Encoding,
			PayloadBytes:   append([]byte(nil), payload.PayloadBytes...),
			PayloadJSON:    append([]byte(nil), payload.PayloadJSON...),
			RedactionState: payload.RedactionState, SizeBytes: payload.SizeBytes,
			ExpiresAt: cloneTime(payload.ExpiresAt), CreatedAt: payload.CreatedAt,
		})
	}
	projected := projectSpan(span)
	if len(payloads) == 0 && (projected.ContentState == telemetry.ContentStateCaptured ||
		projected.ContentState == telemetry.ContentStateTruncated) {
		projected.ContentState = telemetry.ContentStateExpired
	}
	if span.Attributes == nil {
		span.Attributes = map[string]any{}
	}
	if span.Resource == nil {
		span.Resource = map[string]any{}
	}
	return model.TraceSpanDetailEnvelope{
		Data: model.TraceSpanDetail{
			Span: projected, StatusMessage: span.StatusMessage, Attributes: span.Attributes,
			Resource: span.Resource, Events: events, Payloads: payloadDetails,
		},
		Meta: service.meta(),
	}, nil
}

func validFilter(filter Filter) bool {
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 512 || len(filter.Query) > 200 {
		return false
	}
	if len(filter.AgentID) > 256 || len(filter.SessionID) > 256 || len(filter.TaskID) > 256 {
		return false
	}
	if filter.Status != "" && filter.Status != "running" && filter.Status != "succeeded" &&
		filter.Status != "failed" && filter.Status != "unknown" {
		return false
	}
	if filter.Completeness != "" && filter.Completeness != "verified" && filter.Completeness != "partial" {
		return false
	}
	return filter.StartedAfter == nil || filter.StartedBefore == nil || filter.StartedAfter.Before(*filter.StartedBefore)
}

func traceReadError(err error) error {
	if errors.Is(err, storage.ErrTraceNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
}

func (service *Service) meta() model.Meta {
	return model.Meta{FetchedAt: service.now().UTC(), Stale: false, Partial: false}
}

func projectSpan(span telemetry.Span) model.TraceSpan {
	return model.TraceSpan{
		TraceID: span.TraceID, SpanID: span.SpanID, ParentSpanID: span.ParentSpanID, TraceState: span.TraceState,
		Name: span.Name, OpenInferenceKind: span.OpenInferenceKind, OTelSpanKind: span.OTelSpanKind,
		StartedAt: span.StartedAt, EndedAt: cloneTime(span.EndedAt), DurationMS: cloneInt64(span.DurationMS),
		StatusCode: span.StatusCode, AgentID: span.AgentID, SessionID: span.SessionID, TaskID: span.TaskID,
		Provider: span.Provider, Model: span.Model, ToolName: span.ToolName, ToolKind: span.ToolKind,
		MCPServer: span.MCPServer, PeerAgentID: span.PeerAgentID,
		InputTokens: cloneInt64(span.InputTokens), OutputTokens: cloneInt64(span.OutputTokens), TotalTokens: cloneInt64(span.TotalTokens),
		Countable: span.Countable, ContentState: span.ContentState,
		InstrumentationScope: span.InstrumentationScope, InstrumentationVersion: span.InstrumentationVersion,
		SemanticConventionVersion: span.SemanticConventionVersion, ReceivedAt: span.ReceivedAt, UpdatedAt: span.UpdatedAt,
	}
}

func coverage(spans []telemetry.Span) model.TraceCoverage {
	agents, peers, providers, models, mcpServers := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	kinds, scopes, states := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, span := range spans {
		addCoverage(agents, span.AgentID)
		addCoverage(peers, span.PeerAgentID)
		addCoverage(providers, span.Provider)
		addCoverage(models, span.Model)
		addCoverage(mcpServers, span.MCPServer)
		addCoverage(kinds, span.OpenInferenceKind)
		addCoverage(scopes, span.InstrumentationScope)
		addCoverage(states, span.ContentState)
	}
	return model.TraceCoverage{
		Source: coverageSource, AgentIDs: sortedCoverage(agents), PeerAgentIDs: sortedCoverage(peers), Providers: sortedCoverage(providers),
		Models: sortedCoverage(models), MCPServers: sortedCoverage(mcpServers), SpanKinds: sortedCoverage(kinds),
		InstrumentationScopes: sortedCoverage(scopes), ContentStates: sortedCoverage(states),
	}
}

func addCoverage(values map[string]struct{}, value string) {
	if value = strings.TrimSpace(value); value != "" {
		values[value] = struct{}{}
	}
}

func sortedCoverage(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
