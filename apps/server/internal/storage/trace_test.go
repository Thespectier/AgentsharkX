package storage

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestTraceCursorBindsNormalizedFilters(t *testing.T) {
	t.Parallel()
	after := time.Date(2026, 7, 30, 8, 0, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))
	hasError := true
	filter := TraceFilter{
		Status: " SUCCEEDED ", TaskID: "task-a", Query: "TRACE-A", HasError: &hasError,
		StartedAfter: &after,
	}
	encoded, err := EncodeTraceCursor(9, 5, filter)
	if err != nil {
		t.Fatal(err)
	}
	equivalentAfter := after.UTC().Truncate(time.Microsecond)
	equivalent := TraceFilter{
		Status: "succeeded", TaskID: "task-a", Query: "trace-a", HasError: &hasError,
		StartedAfter: &equivalentAfter,
	}
	watermark, sequence, err := DecodeTraceCursor(encoded, equivalent)
	if err != nil || watermark != 9 || sequence != 5 {
		t.Fatalf("normalized cursor = watermark %d sequence %d error %v", watermark, sequence, err)
	}
	changed := equivalent
	changed.TaskID = "task-b"
	if _, _, err := DecodeTraceCursor(encoded, changed); !errors.Is(err, ErrInvalidTraceCursor) {
		t.Fatalf("cursor reused with changed filters: %v", err)
	}
}

func TestNormalizeTraceFilterUsesStorageTimestampPrecision(t *testing.T) {
	t.Parallel()
	after := time.Date(2026, 7, 30, 8, 0, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))
	before := after.Add(time.Second + 987*time.Nanosecond)
	filter := NormalizeTraceFilter(TraceFilter{StartedAfter: &after, StartedBefore: &before})
	if filter.StartedAfter == nil || filter.StartedBefore == nil ||
		!filter.StartedAfter.Equal(after.UTC().Truncate(time.Microsecond)) ||
		!filter.StartedBefore.Equal(before.UTC().Truncate(time.Microsecond)) ||
		filter.StartedAfter.Nanosecond()%1000 != 0 || filter.StartedBefore.Nanosecond()%1000 != 0 {
		t.Fatalf("normalized Trace time bounds = %#v", filter)
	}
}

func TestTraceCursorRejectsMalformedDocuments(t *testing.T) {
	t.Parallel()
	fingerprint, err := traceFilterFingerprint(TraceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for name, document := range map[string]string{
		"unknown field":     fmt.Sprintf(`{"v":1,"watermark":9,"sequence":5,"filter":%q,"extra":true}`, fingerprint),
		"invalid position":  fmt.Sprintf(`{"v":1,"watermark":5,"sequence":9,"filter":%q}`, fingerprint),
		"trailing document": fmt.Sprintf(`{"v":1,"watermark":9,"sequence":5,"filter":%q} {}`, fingerprint),
	} {
		t.Run(name, func(t *testing.T) {
			encoded := base64.RawURLEncoding.EncodeToString([]byte(document))
			if _, _, err := DecodeTraceCursor(encoded, TraceFilter{}); !errors.Is(err, ErrInvalidTraceCursor) {
				t.Fatalf("DecodeTraceCursor error = %v", err)
			}
		})
	}
}

func TestNormalizeTraceGraphLimits(t *testing.T) {
	t.Parallel()
	defaults := NormalizeTraceGraphLimits(TraceGraphLimits{})
	if defaults.SpanLimit != DefaultTraceGraphSpanLimit || defaults.LinkLimit != DefaultTraceGraphLinkLimit {
		t.Fatalf("default limits = %#v", defaults)
	}
	capped := NormalizeTraceGraphLimits(TraceGraphLimits{SpanLimit: MaxTraceGraphSpanLimit + 1, LinkLimit: MaxTraceGraphLinkLimit + 1})
	if capped.SpanLimit != MaxTraceGraphSpanLimit || capped.LinkLimit != MaxTraceGraphLinkLimit {
		t.Fatalf("capped limits = %#v", capped)
	}
}
