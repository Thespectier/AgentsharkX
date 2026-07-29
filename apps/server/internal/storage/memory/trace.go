package memory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/assembler"
)

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
		store.traceSummaries[traceID] = summary
	}
	return result, nil
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

func (store *Store) GetTraceSummary(_ context.Context, traceID string) (telemetry.Summary, error) {
	if !telemetry.ValidTraceID(traceID) {
		return telemetry.Summary{}, storage.ErrTraceNotFound
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
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
