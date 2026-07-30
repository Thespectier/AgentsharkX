package memory

import (
	"context"
	"sort"
	"strings"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
)

func (store *Store) CreateDemoRun(_ context.Context, run model.DemoRun, event model.DemoRunEvent) (model.DemoRun, int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, item := range store.demoRuns {
		if item.run.RequestID == run.RequestID {
			return cloneDemoRun(item.run), 0, nil
		}
	}
	for _, item := range store.demoRuns {
		if demoStatusActive(item.run.Status) {
			return model.DemoRun{}, 0, storage.ErrDemoRunBusy
		}
	}
	if _, exists := store.demoRuns[run.RunID]; exists {
		return model.DemoRun{}, 0, storage.ErrDemoRunConflict
	}
	store.nextDemoListSequence++
	run.RunVersion = 0
	store.demoRuns[run.RunID] = storedDemoRun{sequence: store.nextDemoListSequence, run: cloneDemoRun(run)}
	sequence := store.appendDemoEventLocked(run.RunID, event)
	return cloneDemoRun(run), sequence, nil
}

func (store *Store) UpdateDemoRun(_ context.Context, mutation storage.DemoMutation) (model.DemoRun, int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.demoRuns[mutation.Run.RunID]
	if !exists {
		return model.DemoRun{}, 0, storage.ErrDemoRunNotFound
	}
	if current.run.RunVersion != mutation.ExpectedVersion {
		return model.DemoRun{}, 0, storage.ErrDemoRunConflict
	}
	updated := cloneDemoRun(mutation.Run)
	updated.RunVersion = mutation.ExpectedVersion + 1
	mutation.Event.RunVersion = updated.RunVersion
	store.demoRuns[updated.RunID] = storedDemoRun{sequence: current.sequence, run: updated}
	sequence := int64(0)
	if mutation.Event.Type != "" {
		sequence = store.appendDemoEventLocked(updated.RunID, mutation.Event)
	}
	return cloneDemoRun(updated), sequence, nil
}

func (store *Store) GetDemoRun(_ context.Context, runID string) (model.DemoRun, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	item, exists := store.demoRuns[strings.TrimSpace(runID)]
	if !exists {
		return model.DemoRun{}, storage.ErrDemoRunNotFound
	}
	return cloneDemoRun(item.run), nil
}

func (store *Store) GetDemoRunByRequestID(_ context.Context, requestID string) (model.DemoRun, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, item := range store.demoRuns {
		if item.run.RequestID == strings.TrimSpace(requestID) {
			return cloneDemoRun(item.run), nil
		}
	}
	return model.DemoRun{}, storage.ErrDemoRunNotFound
}

func (store *Store) GetActiveDemoRun(_ context.Context) (model.DemoRun, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, item := range store.demoRuns {
		if demoStatusActive(item.run.Status) {
			return cloneDemoRun(item.run), nil
		}
	}
	return model.DemoRun{}, storage.ErrDemoRunNotFound
}

func (store *Store) ListDemoRuns(_ context.Context, filter storage.DemoRunFilter) (storage.Page[model.DemoRun], error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	limit := storage.NormalizeLimit(filter.Limit)
	watermark := store.nextDemoListSequence
	position := int64(0)
	if filter.Cursor != "" {
		var err error
		watermark, position, err = storage.DecodeDemoCursor(filter.Cursor)
		if err != nil {
			return storage.Page[model.DemoRun]{}, err
		}
	}
	items := make([]storedDemoRun, 0, len(store.demoRuns))
	for _, item := range store.demoRuns {
		if item.sequence <= watermark && (position == 0 || item.sequence < position) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left, right int) bool { return items[left].sequence > items[right].sequence })
	total := 0
	for _, item := range store.demoRuns {
		if item.sequence <= watermark {
			total++
		}
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := storage.Page[model.DemoRun]{Items: make([]model.DemoRun, 0, len(items)), Total: total}
	for _, item := range items {
		page.Items = append(page.Items, cloneDemoRun(item.run))
	}
	if hasMore {
		next, err := storage.EncodeDemoCursor(watermark, items[len(items)-1].sequence)
		if err != nil {
			return storage.Page[model.DemoRun]{}, err
		}
		page.NextCursor = &next
	}
	return page, nil
}

func (store *Store) ReplayDemoAfter(_ context.Context, runID string, after int64, limit int) (storage.ReplayBatch, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.demoRuns[runID]; !exists {
		return storage.ReplayBatch{}, storage.ErrDemoRunNotFound
	}
	if limit < 1 {
		limit = 1000
	}
	batch := storage.ReplayBatch{Messages: []storage.OutboxMessage{}}
	for _, message := range store.outbox {
		if message.Topic != "demo.run" || message.EntityID != runID {
			continue
		}
		if batch.Oldest == 0 {
			batch.Oldest = message.Sequence
		}
		batch.Latest = message.Sequence
		if message.Sequence <= after || len(batch.Messages) >= limit {
			continue
		}
		batch.Messages = append(batch.Messages, cloneDemoMessage(message))
	}
	return batch, nil
}

func (store *Store) GetDemoStreamSnapshot(_ context.Context, runID string) (storage.DemoStreamSnapshot, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	item, exists := store.demoRuns[runID]
	if !exists {
		return storage.DemoStreamSnapshot{}, storage.ErrDemoRunNotFound
	}
	var latest int64
	for _, message := range store.outbox {
		if message.Topic == "demo.run" && message.EntityID == runID {
			latest = message.Sequence
		}
	}
	return storage.DemoStreamSnapshot{Run: cloneDemoRun(item.run), LatestSequence: latest}, nil
}

func (store *Store) appendDemoEventLocked(runID string, event model.DemoRunEvent) int64 {
	store.nextOutboxSequence++
	now := store.options.Now().UTC()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	expiresAt := now.Add(store.options.OutboxRetention)
	store.outbox = append(store.outbox, storage.OutboxMessage{
		Sequence: store.nextOutboxSequence, Topic: "demo.run", EntityID: runID,
		EventKind: event.Type, Demo: &event, CreatedAt: now, ExpiresAt: &expiresAt,
	})
	return store.nextOutboxSequence
}

func cloneDemoMessage(message storage.OutboxMessage) storage.OutboxMessage {
	if message.Demo != nil {
		event := *message.Demo
		if event.Approval != nil {
			approval := *event.Approval
			approval.MatchedRules = append([]string(nil), approval.MatchedRules...)
			event.Approval = &approval
		}
		message.Demo = &event
	}
	if message.ExpiresAt != nil {
		expiresAt := *message.ExpiresAt
		message.ExpiresAt = &expiresAt
	}
	return message
}

func cloneDemoRun(run model.DemoRun) model.DemoRun {
	if run.StartedAt != nil {
		value := *run.StartedAt
		run.StartedAt = &value
	}
	if run.CompletedAt != nil {
		value := *run.CompletedAt
		run.CompletedAt = &value
	}
	if run.LastHeartbeatAt != nil {
		value := *run.LastHeartbeatAt
		run.LastHeartbeatAt = &value
	}
	if run.Approval != nil {
		value := *run.Approval
		value.MatchedRules = append([]string(nil), value.MatchedRules...)
		run.Approval = &value
	}
	if run.ObservedMetrics != nil {
		value := *run.ObservedMetrics
		run.ObservedMetrics = &value
	}
	return run
}

func demoStatusActive(status model.DemoRunStatus) bool {
	return status == model.DemoRunQueued || status == model.DemoRunStarting ||
		status == model.DemoRunRunning || status == model.DemoRunWaitingApproval
}

var _ storage.DemoStore = (*Store)(nil)
