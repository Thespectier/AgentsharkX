// Package stream fans out normalized SSE events without buffering sensitive payloads.
package stream

import (
	"sync"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan model.UnifiedEvent]struct{}
}

func NewHub() *Hub { return &Hub{subscribers: make(map[chan model.UnifiedEvent]struct{})} }

func (hub *Hub) Subscribe() (<-chan model.UnifiedEvent, func()) {
	channel := make(chan model.UnifiedEvent, 8)
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

func (hub *Hub) Publish(event model.UnifiedEvent) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for subscriber := range hub.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}
