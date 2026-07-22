// Package stream stores and fans out a bounded stream of normalized events.
package stream

import (
	"sync"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const DefaultCapacity = 1000

type Record struct {
	Sequence uint64
	Event    model.UnifiedEvent
}

type Hub struct {
	mu          sync.RWMutex
	capacity    int
	sequence    uint64
	records     []Record
	seen        map[string]struct{}
	subscribers map[chan Record]struct{}
}

func NewHub() *Hub { return NewHubWithCapacity(DefaultCapacity) }

func NewHubWithCapacity(capacity int) *Hub {
	if capacity < 1 {
		capacity = 1
	}
	return &Hub{
		capacity: capacity, records: make([]Record, 0, capacity), seen: make(map[string]struct{}, capacity),
		subscribers: make(map[chan Record]struct{}),
	}
}

// Subscribe atomically registers a listener and returns buffered records newer
// than the supplied SSE sequence. Callers can write replay before consuming the
// live channel without opening a gap.
func (hub *Hub) Subscribe(after uint64) (<-chan Record, []Record, func()) {
	hub.mu.Lock()
	if after > hub.sequence {
		// The server restarted or the client supplied a sequence from a
		// different stream epoch. Replay the retained window safely.
		after = 0
	}
	channel := make(chan Record, hub.capacity)
	hub.subscribers[channel] = struct{}{}
	replay := make([]Record, 0, len(hub.records))
	for _, record := range hub.records {
		if record.Sequence > after {
			replay = append(replay, record)
		}
	}
	hub.mu.Unlock()
	return channel, replay, func() {
		hub.mu.Lock()
		if _, ok := hub.subscribers[channel]; ok {
			delete(hub.subscribers, channel)
			close(channel)
		}
		hub.mu.Unlock()
	}
}

// Publish adds an event once per source/upstream identity. The retained event
// and dedupe sets are both bounded by capacity.
func (hub *Hub) Publish(event model.UnifiedEvent) bool {
	key := string(event.Source) + "\x00" + event.ID
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if _, ok := hub.seen[key]; ok {
		return false
	}
	hub.sequence++
	record := Record{Sequence: hub.sequence, Event: event}
	if len(hub.records) == hub.capacity {
		oldest := hub.records[0]
		delete(hub.seen, string(oldest.Event.Source)+"\x00"+oldest.Event.ID)
		copy(hub.records, hub.records[1:])
		hub.records[len(hub.records)-1] = record
	} else {
		hub.records = append(hub.records, record)
	}
	hub.seen[key] = struct{}{}
	for subscriber := range hub.subscribers {
		select {
		case subscriber <- record:
		default:
			// A slow subscriber retains the newest bounded window and can also
			// resume from the server-side ring after reconnecting.
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- record:
			default:
			}
		}
	}
	return true
}

func (hub *Hub) Snapshot() []model.UnifiedEvent {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	events := make([]model.UnifiedEvent, 0, len(hub.records))
	for index := len(hub.records) - 1; index >= 0; index-- {
		events = append(events, hub.records[index].Event)
	}
	return events
}

func (hub *Hub) Len() int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return len(hub.records)
}
