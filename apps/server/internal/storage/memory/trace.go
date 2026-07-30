package memory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/assembler"
)

type sequencedTraceSummary struct {
	sequence int64
	summary  telemetry.Summary
}

func (store *Store) WriteBatch(_ context.Context, batch telemetry.TraceBatch) (telemetry.WriteResult, error) {
	now := store.options.Now().UTC()
	spans := make([]telemetry.Span, 0, len(batch.Spans))
	traceIDs := make(map[string]struct{}, len(batch.Spans))
	for _, span := range batch.Spans {
		prepared, err := telemetry.PrepareSpan(span, now)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		prepared, err = cloneTraceSpan(prepared)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		spans = append(spans, prepared)
		traceIDs[prepared.TraceID] = struct{}{}
	}
	links := make([]telemetry.Link, 0, len(batch.Links))
	for _, link := range batch.Links {
		prepared, err := telemetry.PrepareLink(link)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		prepared, err = cloneTraceLink(prepared)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		links = append(links, prepared)
		traceIDs[prepared.TraceID] = struct{}{}
	}
	payloads := make([]telemetry.Payload, 0, len(batch.Payloads))
	for _, payload := range batch.Payloads {
		prepared, err := telemetry.PreparePayload(payload, now)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		payloads = append(payloads, prepared)
		traceIDs[prepared.TraceID] = struct{}{}
	}
	result := telemetry.WriteResult{TraceIDs: telemetry.SortedTraceIDs(traceIDs)}
	if len(traceIDs) == 0 {
		return result, nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	availableSpans := make(map[string]struct{}, len(store.traceSpans)+len(spans))
	for key := range store.traceSpans {
		availableSpans[key] = struct{}{}
	}
	for _, span := range spans {
		availableSpans[traceSpanKey(span.TraceID, span.SpanID)] = struct{}{}
	}
	for _, link := range links {
		if _, exists := availableSpans[traceSpanKey(link.TraceID, link.SpanID)]; !exists {
			return telemetry.WriteResult{}, errors.New("trace link source span was not found")
		}
	}
	for _, payload := range payloads {
		if _, exists := availableSpans[traceSpanKey(payload.TraceID, payload.SpanID)]; !exists {
			return telemetry.WriteResult{}, errors.New("trace payload source span was not found")
		}
	}

	affectedSummaries := make(map[string]struct{})
	staleSpans := make(map[string]struct{})
	for _, incoming := range spans {
		key := traceSpanKey(incoming.TraceID, incoming.SpanID)
		existing, exists := store.traceSpans[key]
		if !exists {
			store.traceSpans[key] = incoming
			result.Inserted++
			affectedSummaries[incoming.TraceID] = struct{}{}
			continue
		}
		stale := existing.EndedAt != nil && (incoming.EndedAt == nil || incoming.EndedAt.Before(*existing.EndedAt))
		merged := telemetry.MergeSpan(existing, incoming)
		if telemetry.EqualSpan(existing, merged) {
			result.Duplicates++
			if stale {
				staleSpans[key] = struct{}{}
			}
			continue
		}
		store.traceSpans[key] = merged
		result.Updated++
		affectedSummaries[incoming.TraceID] = struct{}{}
	}
	for _, link := range links {
		if _, blocked := staleSpans[traceSpanKey(link.TraceID, link.SpanID)]; blocked {
			continue
		}
		store.traceLinks[traceLinkKey(link)] = link
	}
	for _, payload := range payloads {
		if _, blocked := staleSpans[traceSpanKey(payload.TraceID, payload.SpanID)]; blocked {
			continue
		}
		key := tracePayloadKey(payload.TraceID, payload.SpanID, payload.Kind)
		if existing, exists := store.tracePayloads[key]; exists {
			if existing.ExpiresAt != nil && !existing.ExpiresAt.After(now) {
				continue
			}
			payload.CreatedAt = existing.CreatedAt
			payload.ExpiresAt = existing.ExpiresAt
		}
		store.tracePayloads[key] = payload
	}
	for _, traceID := range telemetry.SortedTraceIDs(affectedSummaries) {
		traceSpans := store.traceSpansLocked(traceID)
		summary, err := assembler.Assemble(traceID, traceSpans, now)
		if err != nil {
			return telemetry.WriteResult{}, err
		}
		if existing, exists := store.traceSummaries[traceID]; exists && summary.RiskLevel == "" {
			summary.RiskLevel = existing.RiskLevel
		}
		if _, exists := store.traceSummarySequences[traceID]; !exists {
			store.nextTraceSummarySequence++
			store.traceSummarySequences[traceID] = store.nextTraceSummarySequence
		}
		store.traceSummaries[traceID] = summary
		store.nextOutboxSequence++
		expiresAt := now.Add(store.options.OutboxRetention)
		outboxSummary := cloneTraceSummary(summary)
		store.outbox = append(store.outbox, storage.OutboxMessage{
			Sequence: store.nextOutboxSequence, Topic: "trace", EntityID: traceID, EventKind: "trace",
			Trace: &outboxSummary, CreatedAt: now, ExpiresAt: &expiresAt,
		})
	}
	return result, nil
}

func (store *Store) ListTraceSummaries(_ context.Context, filter storage.TraceFilter) (storage.Page[telemetry.Summary], error) {
	filter = storage.NormalizeTraceFilter(filter)
	limit := storage.NormalizeLimit(filter.Limit)
	store.mu.RLock()
	defer store.mu.RUnlock()

	watermark := store.nextTraceSummarySequence
	position := int64(0)
	if filter.Cursor != "" {
		var err error
		watermark, position, err = storage.DecodeTraceCursor(filter.Cursor, filter)
		if err != nil {
			return storage.Page[telemetry.Summary]{}, err
		}
	}
	if watermark == 0 {
		return storage.Page[telemetry.Summary]{Items: []telemetry.Summary{}}, nil
	}

	items := make([]sequencedTraceSummary, 0, len(store.traceSummaries))
	for traceID, summary := range store.traceSummaries {
		sequence := store.traceSummarySequences[traceID]
		if sequence < 1 || sequence > watermark || !traceSummaryMatches(summary, filter) {
			continue
		}
		items = append(items, sequencedTraceSummary{sequence: sequence, summary: cloneTraceSummary(summary)})
	}
	sort.Slice(items, func(left, right int) bool { return items[left].sequence > items[right].sequence })
	total := len(items)
	if position > 0 {
		remaining := items[:0]
		for _, item := range items {
			if item.sequence < position {
				remaining = append(remaining, item)
			}
		}
		items = remaining
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := storage.Page[telemetry.Summary]{Items: make([]telemetry.Summary, 0, len(items)), Total: total}
	for _, item := range items {
		page.Items = append(page.Items, item.summary)
	}
	if hasMore {
		next, err := storage.EncodeTraceCursor(watermark, items[len(items)-1].sequence, filter)
		if err != nil {
			return storage.Page[telemetry.Summary]{}, err
		}
		page.NextCursor = &next
	}
	return page, nil
}

func traceSummaryMatches(summary telemetry.Summary, filter storage.TraceFilter) bool {
	if filter.Status != "" && summary.Status != filter.Status ||
		filter.Completeness != "" && summary.Completeness != filter.Completeness ||
		filter.AgentID != "" && summary.RootAgentID != filter.AgentID ||
		filter.SessionID != "" && summary.SessionID != filter.SessionID ||
		filter.TaskID != "" && summary.TaskID != filter.TaskID {
		return false
	}
	if filter.HasError != nil && (summary.ErrorCount > 0) != *filter.HasError ||
		filter.HasA2A != nil && (summary.A2ACalls > 0) != *filter.HasA2A {
		return false
	}
	if filter.StartedAfter != nil && summary.StartedAt.Before(*filter.StartedAfter) ||
		filter.StartedBefore != nil && !summary.StartedAt.Before(*filter.StartedBefore) {
		return false
	}
	if filter.Query != "" {
		query := strings.ToLower(filter.Query)
		if !strings.Contains(strings.ToLower(summary.TraceID), query) &&
			!strings.Contains(strings.ToLower(summary.TaskID), query) &&
			!strings.Contains(strings.ToLower(summary.SessionID), query) {
			return false
		}
	}
	return true
}

func cloneTraceSummary(summary telemetry.Summary) telemetry.Summary {
	if summary.EndedAt != nil {
		endedAt := *summary.EndedAt
		summary.EndedAt = &endedAt
	}
	if summary.DurationMS != nil {
		duration := *summary.DurationMS
		summary.DurationMS = &duration
	}
	return summary
}

func (store *Store) GetTraceSpans(_ context.Context, traceID string) ([]telemetry.Span, error) {
	if !telemetry.ValidTraceID(traceID) {
		return nil, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	spans := store.traceSpansLocked(traceID)
	if len(spans) == 0 {
		return nil, storage.ErrTraceNotFound
	}
	result := make([]telemetry.Span, 0, len(spans))
	for _, span := range spans {
		cloned, err := cloneTraceSpan(span)
		if err != nil {
			return nil, err
		}
		result = append(result, cloned)
	}
	return result, nil
}

func (store *Store) GetTraceSpan(_ context.Context, traceID, spanID string) (telemetry.Span, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return telemetry.Span{}, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.traceSpanLocked(traceID, spanID)
}

func (store *Store) traceSpanLocked(traceID, spanID string) (telemetry.Span, error) {
	span, exists := store.traceSpans[traceSpanKey(traceID, spanID)]
	if !exists {
		return telemetry.Span{}, storage.ErrTraceNotFound
	}
	return cloneTraceSpan(span)
}

func (store *Store) traceSpansLocked(traceID string) []telemetry.Span {
	spans := []telemetry.Span{}
	for _, span := range store.traceSpans {
		if span.TraceID == traceID {
			spans = append(spans, span)
		}
	}
	sort.Slice(spans, func(left, right int) bool {
		if spans[left].StartedAt.Equal(spans[right].StartedAt) {
			return spans[left].SpanID < spans[right].SpanID
		}
		return spans[left].StartedAt.Before(spans[right].StartedAt)
	})
	return spans
}

func (store *Store) GetTraceLinks(_ context.Context, traceID string) ([]telemetry.Link, error) {
	if !telemetry.ValidTraceID(traceID) {
		return nil, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	links := []telemetry.Link{}
	for _, link := range store.traceLinks {
		if link.TraceID == traceID {
			cloned, err := cloneTraceLink(link)
			if err != nil {
				return nil, err
			}
			links = append(links, cloned)
		}
	}
	sort.Slice(links, func(left, right int) bool {
		if links[left].SpanID != links[right].SpanID {
			return links[left].SpanID < links[right].SpanID
		}
		if links[left].LinkedTraceID != links[right].LinkedTraceID {
			return links[left].LinkedTraceID < links[right].LinkedTraceID
		}
		return links[left].LinkedSpanID < links[right].LinkedSpanID
	})
	return links, nil
}

func (store *Store) GetTraceGraph(_ context.Context, traceID string, limits storage.TraceGraphLimits) (storage.TraceGraph, error) {
	if !telemetry.ValidTraceID(traceID) {
		return storage.TraceGraph{}, storage.ErrTraceNotFound
	}
	limits = storage.NormalizeTraceGraphLimits(limits)
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.traceGraphLocked(traceID, limits)
}

func (store *Store) traceGraphLocked(traceID string, limits storage.TraceGraphLimits) (storage.TraceGraph, error) {
	if _, exists := store.traceSummaries[traceID]; !exists {
		return storage.TraceGraph{}, storage.ErrTraceNotFound
	}

	allSpans := store.traceSpansLocked(traceID)
	graph := storage.TraceGraph{TotalSpans: len(allSpans), Spans: []telemetry.Span{}, Links: []telemetry.Link{}}
	if len(allSpans) > limits.SpanLimit {
		graph.SpansTruncated = true
		allSpans = allSpans[:limits.SpanLimit]
	}
	selectedSpans := make(map[string]struct{}, len(allSpans))
	for _, span := range allSpans {
		cloned, err := cloneTraceSpan(span)
		if err != nil {
			return storage.TraceGraph{}, err
		}
		graph.Spans = append(graph.Spans, cloned)
		selectedSpans[span.SpanID] = struct{}{}
	}
	allLinks := make([]telemetry.Link, 0)
	for _, link := range store.traceLinks {
		if link.TraceID != traceID {
			continue
		}
		graph.TotalLinks++
		if _, selected := selectedSpans[link.SpanID]; selected {
			allLinks = append(allLinks, link)
		}
	}
	sort.Slice(allLinks, func(left, right int) bool {
		if allLinks[left].SpanID != allLinks[right].SpanID {
			return allLinks[left].SpanID < allLinks[right].SpanID
		}
		if allLinks[left].LinkedTraceID != allLinks[right].LinkedTraceID {
			return allLinks[left].LinkedTraceID < allLinks[right].LinkedTraceID
		}
		return allLinks[left].LinkedSpanID < allLinks[right].LinkedSpanID
	})
	if len(allLinks) > limits.LinkLimit {
		allLinks = allLinks[:limits.LinkLimit]
	}
	graph.LinksTruncated = len(allLinks) < graph.TotalLinks
	for _, link := range allLinks {
		cloned, err := cloneTraceLink(link)
		if err != nil {
			return storage.TraceGraph{}, err
		}
		graph.Links = append(graph.Links, cloned)
	}
	return graph, nil
}

func (store *Store) GetTraceDetail(_ context.Context, traceID string, limits storage.TraceGraphLimits) (storage.TraceDetail, error) {
	if !telemetry.ValidTraceID(traceID) {
		return storage.TraceDetail{}, storage.ErrTraceNotFound
	}
	limits = storage.NormalizeTraceGraphLimits(limits)
	store.mu.RLock()
	defer store.mu.RUnlock()
	summary, err := store.traceSummaryLocked(traceID)
	if err != nil {
		return storage.TraceDetail{}, err
	}
	graph, err := store.traceGraphLocked(traceID, limits)
	if err != nil {
		return storage.TraceDetail{}, err
	}
	detail := storage.TraceDetail{Summary: summary, Graph: graph}
	if summary.RootSpanID != "" {
		root, err := store.traceSpanLocked(traceID, summary.RootSpanID)
		if err != nil {
			return storage.TraceDetail{}, err
		}
		detail.RootSpan = &root
	}
	return detail, nil
}

func (store *Store) GetTraceSummary(_ context.Context, traceID string) (telemetry.Summary, error) {
	if !telemetry.ValidTraceID(traceID) {
		return telemetry.Summary{}, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.traceSummaryLocked(traceID)
}

func (store *Store) traceSummaryLocked(traceID string) (telemetry.Summary, error) {
	summary, exists := store.traceSummaries[traceID]
	if !exists {
		return telemetry.Summary{}, storage.ErrTraceNotFound
	}
	if summary.EndedAt != nil {
		endedAt := *summary.EndedAt
		summary.EndedAt = &endedAt
	}
	if summary.DurationMS != nil {
		duration := *summary.DurationMS
		summary.DurationMS = &duration
	}
	return summary, nil
}

func (store *Store) GetTracePayload(_ context.Context, traceID, spanID, kind string) (telemetry.Payload, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) || kind == "" {
		return telemetry.Payload{}, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	payload, exists := store.tracePayloads[tracePayloadKey(traceID, spanID, kind)]
	if !exists || payload.RedactionState == telemetry.ContentStateExpired ||
		(payload.ExpiresAt != nil && !payload.ExpiresAt.After(store.options.Now().UTC())) {
		return telemetry.Payload{}, storage.ErrTraceNotFound
	}
	payload.PayloadBytes = append([]byte(nil), payload.PayloadBytes...)
	payload.PayloadJSON = append(json.RawMessage(nil), payload.PayloadJSON...)
	if payload.ExpiresAt != nil {
		expiresAt := *payload.ExpiresAt
		payload.ExpiresAt = &expiresAt
	}
	return payload, nil
}

func (store *Store) GetTracePayloads(_ context.Context, traceID, spanID string) ([]telemetry.Payload, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return nil, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.tracePayloadsLocked(traceID, spanID, store.options.Now().UTC())
}

func (store *Store) tracePayloadsLocked(traceID, spanID string, now time.Time) ([]telemetry.Payload, error) {
	if _, exists := store.traceSpans[traceSpanKey(traceID, spanID)]; !exists {
		return nil, storage.ErrTraceNotFound
	}
	payloads := []telemetry.Payload{}
	for _, payload := range store.tracePayloads {
		if payload.TraceID != traceID || payload.SpanID != spanID ||
			payload.RedactionState == telemetry.ContentStateExpired ||
			payload.ExpiresAt != nil && !payload.ExpiresAt.After(now) {
			continue
		}
		payload.PayloadBytes = append([]byte(nil), payload.PayloadBytes...)
		payload.PayloadJSON = append(json.RawMessage(nil), payload.PayloadJSON...)
		if payload.ExpiresAt != nil {
			expiresAt := *payload.ExpiresAt
			payload.ExpiresAt = &expiresAt
		}
		payloads = append(payloads, payload)
	}
	sort.Slice(payloads, func(left, right int) bool { return payloads[left].Kind < payloads[right].Kind })
	return payloads, nil
}

func (store *Store) GetTraceSpanDetail(_ context.Context, traceID, spanID string) (storage.TraceSpanDetail, error) {
	if !telemetry.ValidTraceID(traceID) || !telemetry.ValidSpanID(spanID) {
		return storage.TraceSpanDetail{}, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	now := store.options.Now().UTC()
	span, err := store.traceSpanLocked(traceID, spanID)
	if err != nil {
		return storage.TraceSpanDetail{}, err
	}
	payloads, err := store.tracePayloadsLocked(traceID, spanID, now)
	if err != nil {
		return storage.TraceSpanDetail{}, err
	}
	return storage.TraceSpanDetail{Span: span, Payloads: payloads}, nil
}

func cloneTraceSpan(span telemetry.Span) (telemetry.Span, error) {
	document, err := json.Marshal(span)
	if err != nil {
		return telemetry.Span{}, err
	}
	var cloned telemetry.Span
	if err := decodeJSON(document, &cloned); err != nil {
		return telemetry.Span{}, err
	}
	return cloned, nil
}

func cloneTraceLink(link telemetry.Link) (telemetry.Link, error) {
	document, err := json.Marshal(link)
	if err != nil {
		return telemetry.Link{}, err
	}
	var cloned telemetry.Link
	if err := decodeJSON(document, &cloned); err != nil {
		return telemetry.Link{}, err
	}
	return cloned, nil
}

func traceSpanKey(traceID, spanID string) string { return traceID + "\x00" + spanID }

func traceLinkKey(link telemetry.Link) string {
	return traceSpanKey(link.TraceID, link.SpanID) + "\x00" + link.LinkedTraceID + "\x00" + link.LinkedSpanID
}

func tracePayloadKey(traceID, spanID, kind string) string {
	return traceSpanKey(traceID, spanID) + "\x00" + kind
}

var _ storage.TraceStore = (*Store)(nil)
