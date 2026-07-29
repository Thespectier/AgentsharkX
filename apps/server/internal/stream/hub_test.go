package stream

import (
	"testing"
	"time"
)

func TestHubCoalescesCommitNotifications(t *testing.T) {
	hub := NewHub()
	notifications, unsubscribe := hub.Subscribe()
	if hub.SubscriberCount() != 1 {
		t.Fatalf("subscriber count = %d", hub.SubscriberCount())
	}
	for index := 0; index < 5000; index++ {
		hub.Notify()
	}
	select {
	case <-notifications:
	case <-time.After(time.Second):
		t.Fatal("commit notification was not delivered")
	}
	select {
	case <-notifications:
		t.Fatal("duplicate notifications were not coalesced")
	default:
	}
	unsubscribe()
	if hub.SubscriberCount() != 0 {
		t.Fatalf("subscriber count after unsubscribe = %d", hub.SubscriberCount())
	}
}
