// Package assembler deterministically derives one Trace summary from the
// complete persisted set of spans for that Trace.
package assembler

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusUnknown   = "unknown"

	CompletenessVerified = "verified"
	CompletenessPartial  = "partial"
)

// Assemble recomputes counters instead of applying deltas. This makes summary
// state independent of span arrival order and prevents duplicate exports from
// inflating counts.
func Assemble(traceID string, spans []telemetry.Span, now time.Time) (telemetry.Summary, error) {
	if !telemetry.ValidTraceID(traceID) {
		return telemetry.Summary{}, errors.New("summary trace ID is invalid")
	}
	if len(spans) == 0 {
		return telemetry.Summary{}, errors.New("cannot assemble an empty trace")
	}
	ordered := append([]telemetry.Span(nil), spans...)
	for _, span := range ordered {
		if span.TraceID != traceID {
			return telemetry.Summary{}, errors.New("summary spans must belong to one trace")
		}
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].StartedAt.Equal(ordered[right].StartedAt) {
			return ordered[left].SpanID < ordered[right].SpanID
		}
		return ordered[left].StartedAt.Before(ordered[right].StartedAt)
	})
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC().Truncate(time.Microsecond)
	summary := telemetry.Summary{
		TraceID: traceID, Status: StatusUnknown, Completeness: CompletenessPartial,
		StartedAt: ordered[0].StartedAt, LastSpanAt: ordered[0].StartedAt,
		SpanCount: len(ordered), UpdatedAt: now,
	}

	rootIndexes := make([]int, 0, 1)
	for index, span := range ordered {
		lastAt := span.StartedAt
		if span.EndedAt != nil && span.EndedAt.After(lastAt) {
			lastAt = *span.EndedAt
		}
		if lastAt.After(summary.LastSpanAt) {
			summary.LastSpanAt = lastAt
		}
		if IsTaskRoot(span) {
			rootIndexes = append(rootIndexes, index)
		}

		kind := strings.ToUpper(span.OpenInferenceKind)
		operation := stringAttribute(span.Attributes, "gen_ai.operation.name")
		a2a := operation == "invoke_agent" && strings.TrimSpace(span.PeerAgentID) != "" && span.Countable
		interaction := false
		if span.Countable {
			switch kind {
			case "LLM":
				summary.LLMCalls++
				interaction = true
				if span.InputTokens != nil {
					summary.InputTokens = saturatingTokenAdd(summary.InputTokens, *span.InputTokens)
				}
				if span.OutputTokens != nil {
					summary.OutputTokens = saturatingTokenAdd(summary.OutputTokens, *span.OutputTokens)
				}
			case "TOOL":
				toolKind := strings.ToLower(strings.TrimSpace(span.ToolKind))
				if toolKind == "mcp" {
					if stringAttribute(span.Attributes, telemetry.AttributeMCPMethod) == "tools/call" {
						summary.MCPCalls++
						summary.ToolCalls++
						interaction = true
					}
				} else {
					summary.LocalToolCalls++
					summary.ToolCalls++
					interaction = true
				}
			case "RETRIEVER":
				summary.RetrieverCalls++
				interaction = true
			}
		}
		if a2a {
			summary.A2ACalls++
			interaction = true
		}
		if interaction && span.StatusCode == telemetry.StatusError {
			summary.ErrorCount++
		}
	}
	summary.TotalTokens = saturatingTokenAdd(summary.InputTokens, summary.OutputTokens)

	if len(rootIndexes) == 0 {
		summary.TaskID = consistentValue(ordered, func(span telemetry.Span) string { return span.TaskID })
		summary.SessionID = consistentValue(ordered, func(span telemetry.Span) string { return span.SessionID })
		return summary, nil
	}
	rootIndex := rootIndexes[0]
	for _, candidate := range rootIndexes {
		if ordered[candidate].EndedAt == nil {
			rootIndex = candidate
			break
		}
	}
	root := ordered[rootIndex]
	summary.TaskID = root.TaskID
	summary.SessionID = root.SessionID
	summary.RootAgentID = root.AgentID
	summary.RootSpanID = root.SpanID
	summary.StartedAt = root.StartedAt
	if root.EndedAt == nil {
		summary.Status = StatusRunning
		return summary, nil
	}
	endedAt := *root.EndedAt
	summary.EndedAt = &endedAt
	duration := endedAt.Sub(root.StartedAt).Milliseconds()
	summary.DurationMS = &duration
	if root.StatusCode == telemetry.StatusError {
		summary.Status = StatusFailed
	} else {
		summary.Status = StatusSucceeded
	}
	if len(rootIndexes) == 1 && root.ParentSpanID == "" && root.AgentID != "" &&
		root.SessionID != "" && root.TaskID != "" {
		summary.Completeness = CompletenessVerified
	}
	return summary, nil
}

func saturatingTokenAdd(total, value int64) int64 {
	if value <= 0 {
		return total
	}
	if total >= math.MaxInt64-value {
		return math.MaxInt64
	}
	return total + value
}

func IsTaskRoot(span telemetry.Span) bool {
	if value, ok := span.Attributes[telemetry.AttributeTaskRoot].(bool); ok && value {
		return true
	}
	return strings.EqualFold(stringAttribute(span.Attributes, telemetry.AttributeSpanKind), "task")
}

func stringAttribute(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return strings.TrimSpace(value)
}

func consistentValue(spans []telemetry.Span, value func(telemetry.Span) string) string {
	result := ""
	for _, span := range spans {
		current := strings.TrimSpace(value(span))
		if current == "" {
			continue
		}
		if result != "" && result != current {
			return ""
		}
		result = current
	}
	return result
}
