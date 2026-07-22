package stream

import (
	"fmt"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestHubBoundsDedupeAndResume(t *testing.T) {
	t.Parallel()
	hub := NewHubWithCapacity(1000)
	for index := 0; index < 5000; index++ {
		event := model.UnifiedEvent{
			ID: fmt.Sprintf("gateway:%d", index), Timestamp: time.UnixMilli(int64(index)),
			Source: model.SourceAgentGateway, Kind: "traffic", Severity: "info",
			Summary: "request", RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: fmt.Sprint(index)},
		}
		if !hub.Publish(event) {
			t.Fatalf("event %d unexpectedly deduplicated", index)
		}
	}
	if hub.Len() != 1000 || len(hub.Snapshot()) != 1000 {
		t.Fatalf("ring was not bounded: len=%d", hub.Len())
	}
	if hub.Publish(hub.Snapshot()[0]) {
		t.Fatal("retained duplicate was published")
	}
	channel, replay, unsubscribe := hub.Subscribe(4997)
	defer unsubscribe()
	if len(replay) != 3 || replay[0].Sequence != 4998 || replay[2].Sequence != 5000 {
		t.Fatalf("unexpected resume replay: %#v", replay)
	}
	_, restartedReplay, restartedUnsubscribe := hub.Subscribe(9000)
	restartedUnsubscribe()
	if len(restartedReplay) != 1000 {
		t.Fatalf("future stream sequence did not replay retained epoch: %d", len(restartedReplay))
	}
	next := model.UnifiedEvent{ID: "guard:new", Timestamp: time.Now(), Source: model.SourceAgentGuard, Kind: "audit", Severity: "info", Summary: "new", RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "new"}}
	if !hub.Publish(next) {
		t.Fatal("new event was not published")
	}
	select {
	case record := <-channel:
		if record.Sequence != 5001 || record.Event.ID != next.ID {
			t.Fatalf("unexpected live record: %#v", record)
		}
	case <-time.After(time.Second):
		t.Fatal("live record not delivered")
	}
}
