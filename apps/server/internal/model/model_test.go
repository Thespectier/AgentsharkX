package model

import (
	"testing"
	"time"
)

func TestCurrentTrendWindowCoversTheExactRollingHour(t *testing.T) {
	t.Parallel()

	window := CurrentTrendWindow(time.Date(2026, 7, 24, 8, 2, 13, 123_000_000, time.UTC))
	if !window.From.Equal(time.Date(2026, 7, 24, 7, 2, 13, 0, time.UTC)) {
		t.Fatalf("unexpected start: %s", window.From)
	}
	if !window.To.Equal(time.Date(2026, 7, 24, 8, 2, 13, 0, time.UTC)) {
		t.Fatalf("unexpected end: %s", window.To)
	}
	if window.BucketDuration != 5*time.Minute || window.To.Sub(window.From) != time.Hour {
		t.Fatalf("unexpected window: %#v", window)
	}
}
