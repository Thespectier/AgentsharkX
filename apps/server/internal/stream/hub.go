// Package stream fans out commit notifications for the persistent outbox.
package stream

import "sync"

// Hub deliberately owns no event data or sequence. PostgreSQL is the replay
// source; this process-local signal only wakes connected readers after commit.
type Hub struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan struct{}]struct{})}
}

// NewHubWithCapacity is retained for test-call compatibility; durable replay
// capacity now belongs to PostgreSQL retention rather than the notifier.
func NewHubWithCapacity(_ int) *Hub { return NewHub() }

func (hub *Hub) Subscribe() (<-chan struct{}, func()) {
	channel := make(chan struct{}, 1)
	hub.mu.Lock()
	hub.subscribers[channel] = struct{}{}
	hub.mu.Unlock()
	return channel, func() {
		hub.mu.Lock()
		if _, ok := hub.subscribers[channel]; ok {
			delete(hub.subscribers, channel)
			close(channel)
		}
		hub.mu.Unlock()
	}
}

// Notify coalesces wakeups. A reader always drains the durable outbox from its
// last delivered sequence, so dropping duplicate wakeups cannot drop events.
func (hub *Hub) Notify() {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for subscriber := range hub.subscribers {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}

func (hub *Hub) SubscriberCount() int {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return len(hub.subscribers)
}
