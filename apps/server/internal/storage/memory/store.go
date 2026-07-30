// Package memory provides the explicit test and Mock implementation of the
// persistent Audit contracts. Production wiring uses PostgreSQL.
package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

type Options struct {
	EventRetention   time.Duration
	TraceRetention   time.Duration
	PayloadRetention time.Duration
	OutboxRetention  time.Duration
	Now              func() time.Time
}

type storedEvent struct {
	id       string
	sequence int64
	event    model.UnifiedEvent
	summary  []byte
}

type Store struct {
	mu                       sync.RWMutex
	options                  Options
	events                   map[string]storedEvent
	payloads                 map[string]storage.AuditPayload
	checkpoints              map[string]storage.Checkpoint
	outbox                   []storage.OutboxMessage
	nextEventSequence        int64
	nextOutboxSequence       int64
	traceSpans               map[string]telemetry.Span
	traceLinks               map[string]telemetry.Link
	tracePayloads            map[string]telemetry.Payload
	traceSummaries           map[string]telemetry.Summary
	traceSummarySequences    map[string]int64
	nextTraceSummarySequence int64
}

func New(options Options) *Store {
	if options.EventRetention <= 0 {
		options.EventRetention = 30 * 24 * time.Hour
	}
	if options.TraceRetention <= 0 {
		options.TraceRetention = 30 * 24 * time.Hour
	}
	if options.OutboxRetention <= 0 {
		options.OutboxRetention = 24 * time.Hour
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{
		options: options, events: make(map[string]storedEvent), payloads: make(map[string]storage.AuditPayload),
		checkpoints: make(map[string]storage.Checkpoint), outbox: []storage.OutboxMessage{},
		traceSpans: make(map[string]telemetry.Span), traceLinks: make(map[string]telemetry.Link),
		tracePayloads: make(map[string]telemetry.Payload), traceSummaries: make(map[string]telemetry.Summary),
		traceSummarySequences: make(map[string]int64),
	}
}

func (*Store) Ready(context.Context) error { return nil }

func (store *Store) PersistEvents(_ context.Context, events []model.UnifiedEvent, checkpoint *storage.Checkpoint) ([]storage.PersistResult, error) {
	type preparedEvent struct {
		event, summaryEvent model.UnifiedEvent
		key                 string
		summary, payload    []byte
	}
	prepared := make([]preparedEvent, 0, len(events))
	for _, event := range events {
		key, err := storage.EventIdentity(event)
		if err != nil {
			return nil, err
		}
		summaryEvent := storage.SummaryEvent(event)
		summary, err := json.Marshal(summaryEvent)
		if err != nil {
			return nil, err
		}
		var payload []byte
		if event.Raw != nil && store.options.PayloadRetention > 0 {
			payload, err = json.Marshal(event.Raw)
			if err != nil {
				return nil, err
			}
		}
		prepared = append(prepared, preparedEvent{event: event, summaryEvent: summaryEvent, key: key, summary: summary, payload: payload})
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.options.Now().UTC()
	retentionCutoff := now.Add(-store.options.EventRetention)
	results := make([]storage.PersistResult, 0, len(events))
	for _, item := range prepared {
		current, exists := store.events[item.key]
		if !exists && item.event.Timestamp.UTC().Before(retentionCutoff) {
			results = append(results, storage.PersistResult{})
			continue
		}
		if exists {
			item.summaryEvent.Timestamp = current.event.Timestamp
			var err error
			item.summary, err = json.Marshal(item.summaryEvent)
			if err != nil {
				return nil, err
			}
		}
		changed := !exists || !bytes.Equal(current.summary, item.summary)
		if !exists {
			store.nextEventSequence++
			current = storedEvent{id: fmt.Sprintf("%032x", store.nextEventSequence), sequence: store.nextEventSequence}
		}
		current.event = cloneEvent(item.summaryEvent)
		current.summary = append([]byte(nil), item.summary...)
		store.events[item.key] = current
		if len(item.payload) > 0 {
			if !exists {
				expiresAt := now.Add(store.options.PayloadRetention)
				store.payloads[current.id] = storage.AuditPayload{
					EventID: current.id, ContentType: "application/json", Encoding: "identity",
					PayloadJSON: item.payload, RedactionState: "captured", SizeBytes: int64(len(item.payload)),
					ExpiresAt: &expiresAt, CreatedAt: now,
				}
			} else {
				payload, retained := store.payloads[current.id]
				if retained && (payload.ExpiresAt == nil || payload.ExpiresAt.After(now)) &&
					!bytes.Equal(payload.PayloadJSON, item.payload) {
					payload.ContentType = "application/json"
					payload.Encoding = "identity"
					payload.PayloadBytes = nil
					payload.PayloadJSON = append(json.RawMessage(nil), item.payload...)
					payload.RedactionState = "captured"
					payload.SizeBytes = int64(len(item.payload))
					store.payloads[current.id] = payload
					changed = true
				}
			}
		}
		result := storage.PersistResult{EventID: current.id, Changed: changed}
		if changed {
			store.nextOutboxSequence++
			expiresAt := now.Add(store.options.OutboxRetention)
			result.OutboxSequence = store.nextOutboxSequence
			store.outbox = append(store.outbox, storage.OutboxMessage{
				Sequence: result.OutboxSequence, Topic: "audit", EntityID: item.event.ID, EventKind: item.event.Kind,
				Event: cloneEvent(item.summaryEvent), CreatedAt: now, ExpiresAt: &expiresAt,
			})
		}
		results = append(results, result)
	}
	if checkpoint != nil {
		store.saveCheckpointLocked(*checkpoint, now)
	}
	return results, nil
}

func (store *Store) ListEvents(_ context.Context, filter storage.EventFilter) (storage.Page[model.UnifiedEvent], error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	limit := storage.NormalizeLimit(filter.Limit)
	watermark := store.nextEventSequence
	var cursor storage.Cursor
	if filter.Cursor != "" {
		var err error
		cursor, err = storage.DecodeCursor(filter.Cursor, filter.Source)
		if err != nil {
			return storage.Page[model.UnifiedEvent]{}, err
		}
		watermark = cursor.Watermark
	}
	items := make([]storedEvent, 0, len(store.events))
	for _, item := range store.events {
		if item.sequence > watermark || (filter.Source != "" && item.event.Source != filter.Source) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].event.Timestamp.Equal(items[j].event.Timestamp) {
			return items[i].id > items[j].id
		}
		return items[i].event.Timestamp.After(items[j].event.Timestamp)
	})
	total := len(items)
	if filter.Cursor != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.event.Timestamp.Before(cursor.OccurredAt) ||
				(item.event.Timestamp.Equal(cursor.OccurredAt) && item.id < cursor.ID) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := storage.Page[model.UnifiedEvent]{Items: make([]model.UnifiedEvent, 0, len(items)), Total: total}
	for _, item := range items {
		page.Items = append(page.Items, cloneEvent(item.event))
	}
	if hasMore {
		last := items[len(items)-1]
		next, err := storage.EncodeCursor(storage.Cursor{
			Watermark: watermark, OccurredAt: last.event.Timestamp, ID: last.id, Source: filter.Source,
		})
		if err != nil {
			return storage.Page[model.UnifiedEvent]{}, err
		}
		page.NextCursor = &next
	}
	return page, nil
}

func (store *Store) GetEvent(_ context.Context, source model.Source, eventID string) (model.UnifiedEvent, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	now := store.options.Now().UTC()
	for _, item := range store.events {
		if item.event.Source != source || (item.event.ID != eventID && item.event.RawRef.ID != eventID) {
			continue
		}
		event := cloneEvent(item.event)
		if payload, ok := store.payloads[item.id]; ok && (payload.ExpiresAt == nil || payload.ExpiresAt.After(now)) {
			_ = decodeJSON(payload.PayloadJSON, &event.Raw)
		}
		return event, nil
	}
	return model.UnifiedEvent{}, storage.ErrNotFound
}

func (store *Store) PutPayload(_ context.Context, payload storage.AuditPayload) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	found := false
	for _, event := range store.events {
		if event.id == payload.EventID {
			found = true
			break
		}
	}
	if !found {
		return storage.ErrNotFound
	}
	payload.PayloadBytes = append([]byte(nil), payload.PayloadBytes...)
	payload.PayloadJSON = append(json.RawMessage(nil), payload.PayloadJSON...)
	if payload.CreatedAt.IsZero() {
		payload.CreatedAt = store.options.Now().UTC()
	}
	store.payloads[payload.EventID] = payload
	return nil
}

func (store *Store) GetPayload(_ context.Context, eventID string) (storage.AuditPayload, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	payload, ok := store.payloads[eventID]
	if !ok || (payload.ExpiresAt != nil && !payload.ExpiresAt.After(store.options.Now().UTC())) {
		return storage.AuditPayload{}, storage.ErrNotFound
	}
	payload.PayloadBytes = append([]byte(nil), payload.PayloadBytes...)
	payload.PayloadJSON = append(json.RawMessage(nil), payload.PayloadJSON...)
	return payload, nil
}

func (store *Store) GetCheckpoint(_ context.Context, source string) (storage.Checkpoint, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	checkpoint, ok := store.checkpoints[source]
	if !ok {
		return storage.Checkpoint{}, storage.ErrNotFound
	}
	checkpoint.Cursor = append(json.RawMessage(nil), checkpoint.Cursor...)
	return checkpoint, nil
}

func (store *Store) SaveCheckpoint(_ context.Context, checkpoint storage.Checkpoint) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.saveCheckpointLocked(checkpoint, store.options.Now().UTC())
	return nil
}

func (store *Store) saveCheckpointLocked(checkpoint storage.Checkpoint, now time.Time) {
	checkpoint.Cursor = append(json.RawMessage(nil), checkpoint.Cursor...)
	checkpoint.UpdatedAt = now
	store.checkpoints[checkpoint.Source] = checkpoint
}

func (store *Store) ReplayAfter(_ context.Context, after int64, limit int) (storage.ReplayBatch, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if limit < 1 {
		limit = 1000
	}
	batch := storage.ReplayBatch{Messages: []storage.OutboxMessage{}, Latest: store.nextOutboxSequence}
	if len(store.outbox) > 0 {
		batch.Oldest = store.outbox[0].Sequence
	}
	for _, message := range store.outbox {
		if message.Sequence <= after {
			continue
		}
		message.Event = cloneEvent(message.Event)
		if message.Trace != nil {
			trace := cloneTraceSummary(*message.Trace)
			message.Trace = &trace
		}
		if message.ExpiresAt != nil {
			expiresAt := *message.ExpiresAt
			message.ExpiresAt = &expiresAt
		}
		batch.Messages = append(batch.Messages, message)
		if len(batch.Messages) == limit {
			break
		}
	}
	return batch, nil
}

func (store *Store) PruneAudit(_ context.Context, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	now = now.UTC()
	for key, event := range store.events {
		if event.event.Timestamp.Before(now.Add(-store.options.EventRetention)) {
			delete(store.events, key)
			delete(store.payloads, event.id)
		}
	}
	for eventID, payload := range store.payloads {
		configuredExpiry := store.options.PayloadRetention <= 0 ||
			!payload.CreatedAt.After(now.Add(-store.options.PayloadRetention))
		originalExpiry := payload.ExpiresAt != nil && !payload.ExpiresAt.After(now)
		if configuredExpiry || originalExpiry {
			delete(store.payloads, eventID)
		}
	}
	retained := store.outbox[:0]
	for _, message := range store.outbox {
		configuredExpiry := !message.CreatedAt.After(now.Add(-store.options.OutboxRetention))
		originalExpiry := message.ExpiresAt != nil && !message.ExpiresAt.After(now)
		if !configuredExpiry && !originalExpiry {
			retained = append(retained, message)
		}
	}
	store.outbox = retained
	return nil
}

func (store *Store) PruneTraces(_ context.Context, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	now = now.UTC()
	for key, payload := range store.tracePayloads {
		if payload.ExpiresAt != nil && !payload.ExpiresAt.After(now) {
			payload.PayloadBytes = nil
			payload.PayloadJSON = nil
			payload.RedactionState = telemetry.ContentStateExpired
			payload.SizeBytes = 0
			store.tracePayloads[key] = payload
		}
	}
	for key, span := range store.traceSpans {
		hasExpiredPayload := false
		hasRetainedPayload := false
		for _, payload := range store.tracePayloads {
			if payload.TraceID != span.TraceID || payload.SpanID != span.SpanID {
				continue
			}
			if payload.RedactionState == telemetry.ContentStateExpired {
				hasExpiredPayload = true
			} else {
				hasRetainedPayload = true
			}
		}
		if hasExpiredPayload && !hasRetainedPayload {
			span.ContentState = telemetry.ContentStateExpired
			span.UpdatedAt = now
			store.traceSpans[key] = span
		}
	}
	traceCutoff := now.Add(-store.options.TraceRetention)
	for traceID, summary := range store.traceSummaries {
		if !summary.LastSpanAt.Before(traceCutoff) {
			continue
		}
		delete(store.traceSummaries, traceID)
		delete(store.traceSummarySequences, traceID)
		for key, span := range store.traceSpans {
			if span.TraceID == traceID {
				delete(store.traceSpans, key)
			}
		}
		for key, link := range store.traceLinks {
			if link.TraceID == traceID {
				delete(store.traceLinks, key)
			}
		}
		for key, payload := range store.tracePayloads {
			if payload.TraceID == traceID {
				delete(store.tracePayloads, key)
			}
		}
	}
	return nil
}

func (store *Store) Prune(ctx context.Context, now time.Time) error {
	if err := store.PruneAudit(ctx, now); err != nil {
		return err
	}
	return store.PruneTraces(ctx, now)
}

func cloneEvent(event model.UnifiedEvent) model.UnifiedEvent {
	document, _ := json.Marshal(event)
	var clone model.UnifiedEvent
	_ = decodeJSON(document, &clone)
	return clone
}

func decodeJSON(document []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	return decoder.Decode(destination)
}

var _ storage.AuditStore = (*Store)(nil)
